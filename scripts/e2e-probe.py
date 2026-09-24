#!/usr/bin/env python3
"""Contract probes for `make e2e`: every non-AWS endpoint, compared against golden files.

Each probe's response (status line, every header but Date, body) is normalised and
compared with scripts/e2e-golden/<name>.txt. With E2E_RECORD=1 the files are written
instead. Run by scripts/e2e-local.sh, which provides the stack and the environment.
Standard library only, and a raw socket client so paths reach the server unaltered.
"""
import difflib
import hashlib
import json
import os
import re
import socket
import subprocess
import sys

HOST = os.environ.get("E2E_HOST", "127.0.0.1")
PORT = int(os.environ["E2E_PORT"])
RL_PORT = int(os.environ["E2E_RL_PORT"])
OFF_PORT = int(os.environ["E2E_OFF_PORT"])
TOKEN = os.environ["E2E_TOKEN"]
SCOPED = os.environ["E2E_SCOPED"]  # "<bucket>:<secret>"
APP_URL = os.environ["E2E_APP_URL"]
SELF_URL = os.environ["E2E_SELF_URL"]  # the api as seen from inside the docker network
REDIS = os.environ["E2E_REDIS_CONTAINER"]
RECORD = os.environ.get("E2E_RECORD") == "1"

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
GOLDEN = os.path.join(ROOT, "scripts", "e2e-golden")
FIXTURES = os.path.join(ROOT, "scripts", "e2e-fixtures")
SCOPED_BUCKET, SCOPED_SECRET = SCOPED.split(":", 1)
BUCKET = "golden"
BOUNDARY = "cdne2eBoundary7MA4YWxkTrZu0gW"
AUTH = ("Authorization", "Bearer " + TOKEN)
SCOPED_AUTH = ("Authorization", "Bearer " + SCOPED)
ORIGIN = ("Origin", "https://panel.example.test")
UA = ("User-Agent", "cdn-e2e")


def fixture(name):
    with open(os.path.join(FIXTURES, name), "rb") as f:
        return f.read()


def public(name):
    with open(os.path.join(ROOT, "public", name), "rb") as f:
        return f.read()


PNG, JPG, GIF, WEBP, TIFF = (fixture("photo." + e) for e in ("png", "jpg", "gif", "webp", "tiff"))
PDF = b"%PDF-1.4\n1 0 obj << /Type /Catalog >> endobj\ntrailer << /Root 1 0 R >>\n%%EOF\n"
HEIC = b"\x00\x00\x00\x18ftypheic\x00\x00\x00\x00mif1heic" + bytes(range(256)) * 4
SVG = (b'<svg xmlns="http://www.w3.org/2000/svg" width="10" height="10">'
       b'<script>alert(1)</script><rect width="10" height="10" fill="red"/></svg>')
CSV_MARKUP = b"<script>alert(document.domain)</script>"
SQL = "-- dump\nINSERT INTO t (c) VALUES ('ğüşiöç');\n".encode()
CSV = "id,name,total\n1,\"Ünal, Ayşe\",12.50\n".encode()
PHP = b"<?php echo 1; ?>"
POLYGLOT = PNG + b"<?php system($_GET['c']); ?>"

LABELS = {hashlib.sha256(b).hexdigest(): n for n, b in {
    "fixture photo.png": PNG, "fixture photo.jpg": JPG, "fixture photo.gif": GIF,
    "fixture photo.webp": WEBP, "fixture photo.tiff": TIFF, "fixture doc.pdf": PDF,
    "fixture photo.heic": HEIC, "fixture polyglot.png": POLYGLOT,
    "public/notfound.png": public("notfound.png"), "public/favicon.png": public("favicon.png"),
    "public/scalar.html": public("scalar.html"),
}.items()}


# ---------- raw HTTP/1.1 client ----------

def _read_until(sock, buf, marker):
    while marker not in buf:
        chunk = sock.recv(65536)
        if not chunk:
            break
        buf += chunk
    return buf


def _read_exact(sock, buf, n):
    while len(buf) < n:
        chunk = sock.recv(65536)
        if not chunk:
            break
        buf += chunk
    return buf


def send(method, path, headers=(), body=b"", port=None, content_length=None, timeout=90):
    """One request on a fresh connection. path may be bytes to carry raw UTF-8."""
    port = port or PORT
    if isinstance(path, str):
        path = path.encode("latin-1")
    lines = [method.encode() + b" " + path + b" HTTP/1.1", b"Host: " + ("%s:%d" % (HOST, port)).encode()]
    for k, v in (UA,) + tuple(headers):
        lines.append(k.encode() + b": " + v.encode("utf-8"))
    if content_length is not None:
        lines.append(b"Content-Length: " + str(content_length).encode())
    elif body:
        lines.append(b"Content-Length: " + str(len(body)).encode())
    raw = b"\r\n".join(lines) + b"\r\n\r\n" + body

    sock = socket.create_connection((HOST, port), timeout=timeout)
    upgraded = False
    try:
        sock.sendall(raw)
        buf = _read_until(sock, b"", b"\r\n\r\n")
        head, _, rest = buf.partition(b"\r\n\r\n")
        head_lines = head.decode("latin-1").split("\r\n")
        status = head_lines[0]
        hdrs = []
        for line in head_lines[1:]:
            name, _, value = line.partition(":")
            hdrs.append((name, value.strip()))
        code = int(status.split(" ")[1]) if len(status.split(" ")) > 1 else 0
        lower = {k.lower(): v for k, v in hdrs}
        if code == 101:
            upgraded = True
            sock.settimeout(10)
            return status, hdrs, _read_exact(sock, rest, 2), sock
        if method == "HEAD" or code in (204, 304) or 100 <= code < 200:
            data = b""
        elif "chunked" in lower.get("transfer-encoding", "").lower():
            data = b""
            while True:
                rest = _read_until(sock, rest, b"\r\n")
                size_line, _, rest = rest.partition(b"\r\n")
                size = int(size_line.split(b";")[0], 16)
                rest = _read_exact(sock, rest, size + 2)
                data += rest[:size]
                rest = rest[size + 2:]
                if size == 0:
                    break
        elif "content-length" in lower:
            n = int(lower["content-length"])
            data = _read_exact(sock, rest, n)[:n]
        else:
            while True:
                chunk = sock.recv(65536)
                if not chunk:
                    break
                rest += chunk
            data = rest
        return status, hdrs, data, None
    finally:
        if not upgraded:
            sock.close()


def multipart(fields=(), files=()):
    out = b""
    for name, value in fields:
        out += ("--%s\r\nContent-Disposition: form-data; name=\"%s\"\r\n\r\n" % (BOUNDARY, name)).encode()
        out += value.encode("utf-8") + b"\r\n"
    for field, filename, ctype, data in files:
        out += ("--%s\r\nContent-Disposition: form-data; name=\"%s\"; filename=\"%s\"\r\n" % (BOUNDARY, field, filename)).encode("utf-8")
        if ctype:
            out += ("Content-Type: %s\r\n" % ctype).encode()
        out += b"\r\n" + data + b"\r\n"
    out += ("--%s--\r\n" % BOUNDARY).encode()
    return ("Content-Type", "multipart/form-data; boundary=" + BOUNDARY), out


# ---------- normalisation ----------

UUID_RE = re.compile(r"[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}")
TIME_RE = re.compile(r"\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(\.\d+)?(Z|[+-]\d{2}:\d{2})")
FORMATS_RE = re.compile(r"(Allowed formats: )((?:\.[a-z0-9]+, )*\.[a-z0-9]+)")
HTTPDATE_RE = re.compile(r"^[A-Z][a-z]{2}, \d{2} [A-Z][a-z]{2} \d{4} \d{2}:\d{2}:\d{2} GMT$")


def norm_text(s):
    s = s.replace(APP_URL, "<app>").replace(SELF_URL, "<self>")
    s = s.replace(TOKEN, "<token>").replace(SCOPED_SECRET, "<scoped-secret>")
    s = UUID_RE.sub("<uuid>", s)
    # pkg/validator builds this list from a map, so its order changes per process.
    s = FORMATS_RE.sub(lambda m: m.group(1) + ", ".join(sorted(m.group(2).split(", "))), s)
    return TIME_RE.sub("<time>", s)


def image_dims(b):
    if b[:8] == b"\x89PNG\r\n\x1a\n":
        return "png %dx%d" % (int.from_bytes(b[16:20], "big"), int.from_bytes(b[20:24], "big"))
    if b[:6] in (b"GIF87a", b"GIF89a"):
        return "gif %dx%d" % (int.from_bytes(b[6:8], "little"), int.from_bytes(b[8:10], "little"))
    if b[:2] == b"\xff\xd8":
        i = 2
        while i + 9 < len(b):
            if b[i] != 0xFF:
                break
            marker, seglen = b[i + 1], int.from_bytes(b[i + 2:i + 4], "big")
            if marker in (0xC0, 0xC1, 0xC2):
                return "jpeg %dx%d" % (int.from_bytes(b[i + 7:i + 9], "big"), int.from_bytes(b[i + 5:i + 7], "big"))
            i += 2 + seglen
        return "jpeg"
    if b[:4] == b"RIFF" and b[8:12] == b"WEBP":
        return "webp"
    if b[:4] in (b"II*\x00", b"MM\x00*"):
        return "tiff"
    return None


def render_body(body, probe):
    if not body:
        return "(empty)"
    label = LABELS.get(hashlib.sha256(body).hexdigest())
    if label:
        return "%s (%d bytes)" % (label, len(body))
    if probe.get("body_fn"):
        try:
            return probe["body_fn"](body)
        except (ValueError, KeyError, TypeError, AttributeError):
            pass  # not the expected shape: fall through so the diff shows what came back
    try:
        text = body.decode("utf-8")
        if "\x00" not in text:
            return norm_text(text)
    except UnicodeDecodeError:
        pass
    dims = image_dims(body)
    # ImageMagick stamps PNGs it writes with the time, so only their size is stable.
    if probe.get("volatile_body") or (dims or "").startswith("png"):
        return "binary %s len=%d" % (dims or "unknown", len(body))
    return "binary %s sha256=%s len=%d" % (dims or "unknown", hashlib.sha256(body).hexdigest(), len(body))


# Seconds left in the window depend on timing everywhere; the remaining count only
# where earlier requests of unknown number share the window.
TIMING_HEADERS = {"x-ratelimit-reset", "retry-after"}


def render(probe, status, hdrs, body, extra=""):
    out = ["> %s %s" % (probe["method"], norm_text(probe["shown_path"]))]
    for k, v in probe.get("headers", ()):
        out.append("> %s: %s" % (k, norm_text(v)))
    if probe.get("body_desc"):
        out.append("> body: %s" % probe["body_desc"])
    out.append("< " + status)
    rendered = render_body(body, probe)
    # Go drops trailing zeros from fractional seconds, so a timestamp moves the length.
    timed = "<time>" in rendered
    for k, v in sorted(hdrs, key=lambda kv: kv[0].lower()):
        lk = k.lower()
        if lk == "date":
            if not HTTPDATE_RE.match(v):
                v = "INVALID " + v
            else:
                continue
        elif lk == "last-modified":
            v = "<http-date>" if HTTPDATE_RE.match(v) else "INVALID " + v
        elif lk in TIMING_HEADERS or (lk == "x-ratelimit-remaining" and probe.get("volatile_limits", True)):
            v = "<n>" if v.isdigit() else "INVALID " + v
        elif lk == "content-length" and (probe.get("volatile_body") or probe.get("body_fn") or timed):
            v = "<n>" if v.isdigit() else "INVALID " + v
        out.append("< %s: %s" % (k, norm_text(v)))
    out.append("<")
    out.append(rendered)
    if extra:
        out.append(extra)
    return "\n".join(out) + "\n"


# ---------- body renderers for volatile endpoints ----------

def json_shape(value):
    if isinstance(value, dict) and value and all(k.startswith("/") for k in value):
        return "map of mountpoint to number"  # mounts depend on the docker host
    if isinstance(value, dict):
        return {k: json_shape(v) for k, v in value.items()}
    if isinstance(value, list):
        return [json_shape(v) for v in value[:1]]
    if isinstance(value, bool):
        return "bool"
    if isinstance(value, (int, float)):
        return "number"
    if value is None:
        return None
    return "string"


def shape_body(body):
    return "json shape " + json.dumps(json_shape(json.loads(body)), sort_keys=True)


def sorted_data_body(body):
    doc = json.loads(body)
    doc["data"] = sorted(doc.get("data") or [], key=lambda r: norm_text(json.dumps(r, sort_keys=True)))
    return "json (data sorted) " + norm_text(json.dumps(doc, sort_keys=True, ensure_ascii=False))


def metrics_body(body):
    # Families only. fiber keeps label strings that alias its pooled request buffers,
    # so the recorded label values are corrupted over time and cannot be a contract.
    lines = [l for l in body.decode().splitlines() if re.match(r"^# (HELP|TYPE) cdn_", l)]
    return "\n".join(["cdn metric families:"] + sorted(lines))


# ---------- probes ----------

CTX = {}
PROBES = []


def probe(name, method, path, headers=(), body=b"", **opts):
    PROBES.append(dict(name=name, method=method, path=path, headers=tuple(headers), body=body, **opts))


def up(name, filename, data, ctype=None, fields=None, headers=(AUTH,), capture=None, query="", **opts):
    fields = fields if fields is not None else [("bucket", BUCKET), ("path", "p")]
    ct, body = multipart(fields, [("file", filename, ctype, data)])
    desc = "multipart %s file=%s(%d bytes)" % (",".join("%s=%s" % f for f in fields), filename, len(data))
    probe(name, "POST", "/upload" + query, tuple(headers) + (ct,), body, body_desc=desc, capture=capture, **opts)


def cap_object(key):
    def fn(body):
        CTX[key] = json.loads(body)["data"]["objectName"]
    return fn


def obj(key, prefix="/" + BUCKET + "/", suffix=""):
    return lambda: prefix + CTX.get(key, "missing-" + key) + suffix


def build():
    # service and meta routes
    probe("health_get", "GET", "/health")
    # No body to spot the timestamp in, yet its length still moves Content-Length.
    probe("health_head", "HEAD", "/health", volatile_body=True)
    probe("health_upper", "GET", "/HEALTH")
    probe("health_trailing_slash", "GET", "/health/")
    probe("health_origin", "GET", "/health", [ORIGIN])
    probe("index_get", "GET", "/")
    probe("index_head", "HEAD", "/")
    probe("scalar_yaml", "GET", "/scalar.yaml")
    probe("favicon_get", "GET", "/favicon.ico")
    probe("favicon_head", "HEAD", "/favicon.ico")
    probe("favicon_post", "POST", "/favicon.ico")
    probe("favicon_options", "OPTIONS", "/favicon.ico")
    probe("unmatched_post", "POST", "/nope")
    probe("unmatched_escaped", "POST", "/a&b%3Cc%3E")
    probe("unmatched_put_object", "PUT", "/golden/x.png")
    probe("unmatched_patch_upload", "PATCH", "/upload")
    probe("unknown_method", "FOO", "/health")

    # operator routes (AWS only up to the auth gate)
    probe("metrics_no_token", "GET", "/metrics")
    probe("metrics_scoped_token", "GET", "/metrics", [SCOPED_AUTH])
    probe("monitor_no_token", "GET", "/monitor")
    probe("monitor_token", "GET", "/monitor", [AUTH], body_fn=shape_body)
    probe("aws_no_token", "GET", "/aws/bucket-list")
    probe("aws_scoped_token", "GET", "/aws/bucket-list", [SCOPED_AUTH])
    probe("aws_glacier_no_token", "POST", "/aws/glacier/v/jobs/j/async-download")
    probe("minio_no_token", "GET", "/minio/bucket-list")
    probe("minio_create", "GET", "/minio/golden-empty/create", [AUTH])
    probe("minio_create_again", "GET", "/minio/golden-empty/create", [AUTH])
    probe("minio_list", "GET", "/minio/bucket-list", [AUTH])
    probe("minio_exists", "GET", "/minio/golden-empty/exists", [AUTH])
    probe("minio_exists_missing", "GET", "/minio/golden-nothere/exists", [AUTH])
    probe("minio_delete", "DELETE", "/minio/golden-empty/delete", [AUTH])
    probe("minio_delete_missing", "DELETE", "/minio/golden-empty/delete", [AUTH])

    # websocket gate
    ws_up = [("Connection", "Upgrade"), ("Upgrade", "websocket"), ("Sec-WebSocket-Version", "13"),
             ("Sec-WebSocket-Key", "dGhlIHNhbXBsZSBub25jZQ==")]
    probe("ws_no_upgrade", "GET", "/ws")
    probe("ws_post", "POST", "/ws")
    probe("ws_subpath", "GET", "/ws/extra")
    probe("ws_upgrade_no_token", "GET", "/ws", ws_up)
    probe("ws_upgrade_bad_token", "GET", "/ws?token=wrong", ws_up)
    probe("ws_upgrade_ok", "GET", "/ws?token=" + TOKEN, ws_up, websocket=True)
    probe("ws_upgrade_no_key", "GET", "/ws?token=" + TOKEN, ws_up[:3])
    probe("ws_upgrade_bad_version", "GET", "/ws?token=" + TOKEN, [ws_up[0], ws_up[1], ("Sec-WebSocket-Version", "8"), ws_up[3]])
    probe("ws_upgrade_post", "POST", "/ws?token=" + TOKEN, ws_up)
    probe("ws_upgrade_subpath", "GET", "/wsextra/x.png?token=" + TOKEN, ws_up)

    # uploads
    up("upload_png", "photo.png", PNG, "image/png", capture=cap_object("png"))
    up("upload_jpg", "photo.jpg", JPG, "image/jpeg", capture=cap_object("jpg"))
    up("upload_gif", "photo.gif", GIF, capture=cap_object("gif"))
    up("upload_webp", "photo.webp", WEBP, "application/octet-stream", capture=cap_object("webp"))
    up("upload_tiff", "photo.tiff", TIFF, capture=cap_object("tiff"))
    up("upload_pdf", "doc.pdf", PDF, "application/pdf", capture=cap_object("pdf"))
    up("upload_heic", "IMG_0001.HEIC", HEIC, "image/heic", capture=cap_object("heic"))
    up("upload_svg", "icon.svg", SVG, capture=cap_object("svg"))
    up("upload_csv_markup", "payload.csv", CSV_MARKUP, capture=cap_object("csv"))
    up("upload_sql", "dump.sql", SQL, capture=cap_object("sql"))
    up("upload_polyglot", "poly.png", POLYGLOT, capture=cap_object("poly"))
    up("upload_origin", "photo.png", PNG, headers=(AUTH, ORIGIN))
    up("upload_turkish_path", "photo.png", PNG, fields=[("bucket", BUCKET), ("path", "şehir/iki kelime")])
    up("upload_pct_path", "photo.png", PNG, fields=[("bucket", BUCKET), ("path", "pct%20dir")], capture=cap_object("pct"))
    up("upload_query_bucket", "photo.png", PNG, fields=[("path", "q")], query="?bucket=" + BUCKET)
    up("upload_query_and_form_bucket", "photo.png", PNG, fields=[("bucket", BUCKET)], query="?bucket=goldenq")
    up("upload_optimize", "photo.png", PNG, fields=[("bucket", BUCKET), ("optimize", "true")], volatile_body=True)
    up("upload_width", "photo.png", PNG, fields=[("bucket", BUCKET), ("width", "60")])
    up("upload_no_bucket", "photo.png", PNG, fields=[])
    up("upload_bad_bucket_name", "photo.png", PNG, fields=[("bucket", "Bad_Name")])
    up("upload_php", "shell.php", PHP)
    up("upload_php_as_jpg", "shell.jpg", PHP)
    up("upload_no_extension", "noext", PDF)
    up("upload_no_token", "photo.png", PNG, headers=())
    up("upload_bad_token", "photo.png", PNG, headers=(("Authorization", "Bearer wrong"),))
    up("upload_malformed_auth", "photo.png", PNG, headers=(("Authorization", "Bearer"),))
    up("upload_scoped_own", "photo.png", PNG, fields=[], headers=(SCOPED_AUTH,))
    up("upload_scoped_other", "photo.png", PNG, headers=(SCOPED_AUTH,))
    ct, body = multipart([("bucket", BUCKET)])
    probe("upload_no_file", "POST", "/upload", [AUTH, ct], body, body_desc="multipart bucket only")
    probe("upload_json_body", "POST", "/upload", [AUTH, ("Content-Type", "application/json")], b"{}", body_desc="{}")

    # reads
    for key in ("png", "jpg", "gif", "webp", "tiff", "pdf", "heic", "svg", "csv", "sql", "poly"):
        probe("get_" + key, "GET", obj(key))
    probe("get_png_head", "HEAD", obj("png"))
    probe("get_png_origin", "GET", obj("png"), [ORIGIN])
    probe("get_png_range", "GET", obj("png"), [("Range", "bytes=0-9")])
    probe("get_png_if_modified", "GET", obj("png"), [("If-Modified-Since", "Tue, 01 Jan 2030 00:00:00 GMT")])
    probe("get_w", "GET", obj("png", "/golden/w:40/"))
    probe("get_h", "GET", obj("png", "/golden/h:20/"))
    probe("get_wh", "GET", obj("png", "/golden/w:40/h:20/"))
    probe("get_w_upper", "GET", obj("png", "/golden/W:40/"))
    probe("get_query_width", "GET", obj("png", suffix="?width=40"))
    probe("get_query_width_height", "GET", obj("png", suffix="?width=40&height=20"))
    probe("get_query_negative", "GET", obj("png", suffix="?width=-5"))
    probe("get_query_huge", "GET", obj("png", suffix="?width=99999"))
    for key in ("jpg", "gif", "webp", "tiff", "pdf"):
        probe("get_%s_width" % key, "GET", obj(key, suffix="?width=30"))
    probe("get_pct_raw", "GET", obj("pct"))
    probe("get_pct_double_encoded", "GET", lambda: "/golden/" + CTX.get("pct", "x").replace("%20", "%2520"))
    probe("get_missing_object", "GET", "/golden/does/not/exist.png")
    probe("get_missing_object_head", "HEAD", "/golden/does/not/exist.png")
    probe("get_placeholder_if_modified", "GET", "/golden/does/not/exist.png", [("If-Modified-Since", "Tue, 01 Jan 2030 00:00:00 GMT")])
    probe("get_placeholder_range", "GET", "/golden/does/not/exist.png", [("Range", "bytes=0-9")])
    probe("get_placeholder_range_suffix", "GET", "/golden/does/not/exist.png", [("Range", "bytes=-10")])
    probe("get_placeholder_range_open", "GET", "/golden/does/not/exist.png", [("Range", "bytes=3900-")])
    probe("get_placeholder_range_invalid", "GET", "/golden/does/not/exist.png", [("Range", "bytes=99999-")])
    probe("get_placeholder_range_head", "HEAD", "/golden/does/not/exist.png", [("Range", "bytes=0-9")])
    probe("get_placeholder_if_modified_old", "GET", "/golden/does/not/exist.png", [("If-Modified-Since", "Sat, 01 Jan 2000 00:00:00 GMT")])
    probe("get_placeholder_if_modified_garbage", "GET", "/golden/does/not/exist.png", [("If-Modified-Since", "yesterday")])
    probe("index_range", "GET", "/", [("Range", "bytes=0-4")])
    probe("favicon_upper", "GET", "/FAVICON.ICO")
    probe("health_post", "POST", "/health")
    probe("get_query_not_a_number", "GET", obj("png", suffix="?width=abc"))
    probe("get_query_bad_escape", "GET", obj("png", suffix="?width=%zz&height=20"))
    probe("get_missing_bucket", "GET", "/nobucket/x.png")
    probe("get_traversal", "GET", "/golden/a/../x.png")
    probe("get_bucket_only", "GET", "/golden")
    probe("get_bucket_slash", "GET", "/golden/")
    probe("get_trailing_slash", "GET", obj("png", suffix="/"))
    probe("get_double_slash", "GET", obj("png", prefix="/golden//"))
    probe("get_upper_bucket", "GET", obj("png", prefix="/GOLDEN/"))
    # fiber matches Use and Group prefixes with a plain HasPrefix, no segment boundary
    probe("get_bucket_named_minio", "GET", "/minio/x.png")
    probe("get_bucket_prefixed_minio", "GET", "/minioextra/x.png")
    probe("get_bucket_prefixed_minio_token", "GET", "/minioextra/x.png", [AUTH])
    probe("get_bucket_prefixed_aws", "GET", "/awsextra/x.png")
    probe("get_bucket_prefixed_ws", "GET", "/wsextra/x.png")
    probe("get_bucket_named_metrics", "GET", "/metrics/x.png")
    probe("get_bucket_named_upload", "GET", "/upload/x.png")
    probe("get_raw_utf8", "GET", "/golden/şehir/görüntü.png".encode("utf-8"), shown="/golden/<raw utf-8 şehir/görüntü.png>")

    # resize endpoint
    ct, body = multipart([("width", "30"), ("height", "20")], [("file", "photo.png", "image/png", PNG)])
    probe("resize_no_token", "POST", "/resize", [ct], body, body_desc="png 30x20")
    probe("resize_png", "POST", "/resize", [AUTH, ct], body, body_desc="png 30x20")
    ct, body = multipart([], [("file", "table.csv", "text/csv", CSV)])
    probe("resize_passthrough", "POST", "/resize", [AUTH, ct], body, body_desc="csv, no size")
    ct, body = multipart([("width", "30")])
    probe("resize_no_file", "POST", "/resize", [AUTH, ct], body, body_desc="no file")

    # batch
    ct, body = multipart([("bucket", BUCKET), ("path", "b")], [("files", "a.png", "image/png", PNG), ("files", "b.pdf", None, PDF), ("files", "c.sql", None, SQL)])
    probe("batch_upload", "POST", "/batch/upload", [AUTH, ct], body, body_desc="3 files", body_fn=sorted_data_body, capture=cap_batch)
    ct, body = multipart([("bucket", BUCKET)], [("file", "a.png", "image/png", PNG)])
    probe("batch_upload_wrong_field", "POST", "/batch/upload", [AUTH, ct], body, body_desc="field file")
    ct, body = multipart([("bucket", BUCKET)], [("files", "f%03d.sql" % i, None, b"SELECT 1;") for i in range(101)])
    probe("batch_upload_too_many", "POST", "/batch/upload", [AUTH, ct], body, body_desc="101 files")
    ct, body = multipart([("bucket", "golden-nothere")], [("files", "a.png", "image/png", PNG)])
    probe("batch_upload_missing_bucket", "POST", "/batch/upload", [AUTH, ct], body, body_desc="missing bucket")
    probe("batch_upload_json", "POST", "/batch/upload", [AUTH, ("Content-Type", "application/json")], b"{}", body_desc="{}")
    jct = ("Content-Type", "application/json")
    probe("batch_delete_json", "DELETE", "/batch/delete", [AUTH, jct], lambda: json.dumps({"bucket": BUCKET, "files": CTX.get("batch", [])}).encode(), body_desc="batch objects", body_fn=sorted_data_body)
    probe("batch_delete_form", "DELETE", "/batch/delete", [AUTH, ("Content-Type", "application/x-www-form-urlencoded")], b"bucket=golden&files=nope-a.png&files=nope-b.png", body_desc="form", body_fn=sorted_data_body)
    probe("batch_delete_invalid", "DELETE", "/batch/delete", [AUTH, jct], b"{not-json", body_desc="invalid json")
    probe("batch_delete_no_content_type", "DELETE", "/batch/delete", [AUTH], b'{"bucket":"golden","files":["x"]}', body_desc="no content type")
    probe("batch_delete_no_files", "DELETE", "/batch/delete", [AUTH, jct], b'{"bucket":"golden"}', body_desc="no files")
    probe("batch_delete_too_many", "DELETE", "/batch/delete", [AUTH, jct], json.dumps({"bucket": BUCKET, "files": ["f"] * 101}).encode(), body_desc="101 files")
    probe("batch_delete_no_bucket", "DELETE", "/batch/delete", [AUTH, jct], b'{"files":["x"]}', body_desc="no bucket")
    probe("batch_delete_scoped_other", "DELETE", "/batch/delete", [SCOPED_AUTH, jct], b'{"bucket":"golden","files":["x"]}', body_desc="other bucket")

    # upload from URL (UPLOAD_URL_ALLOW_PRIVATE=true on this container)
    probe("upload_url", "POST", "/upload-url", [AUTH, jct], lambda: json.dumps({"bucket": BUCKET, "path": "u", "url": SELF_URL + "/golden/" + CTX.get("png", "x")}).encode(), body_desc="self png")
    probe("upload_url_form", "POST", "/upload-url", [AUTH, ("Content-Type", "application/x-www-form-urlencoded")], lambda: ("bucket=golden&url=" + SELF_URL + "/golden/" + CTX.get("png", "x")).encode(), body_desc="form, self png")
    probe("upload_url_file_scheme", "POST", "/upload-url", [AUTH, jct], b'{"bucket":"golden","url":"file:///etc/passwd"}', body_desc="file scheme")
    probe("upload_url_invalid", "POST", "/upload-url", [AUTH, jct], b"{not-json", body_desc="invalid json")
    probe("upload_url_no_url", "POST", "/upload-url", [AUTH, jct], b'{"bucket":"golden"}', body_desc="no url")
    probe("upload_url_no_token", "POST", "/upload-url", [jct], b'{"bucket":"golden","url":"http://example.com/a.png"}', body_desc="no token")

    # archive (disabled without AWS)
    probe("archive_disabled", "POST", "/archive", [AUTH, jct], b'{"bucket":"golden","files":["x.png"]}', body_desc="one file")
    probe("archive_invalid", "POST", "/archive", [AUTH, jct], b"{not-json", body_desc="invalid json")
    probe("archive_no_bucket", "POST", "/archive", [AUTH, jct], b'{"files":["x.png"]}', body_desc="no bucket")
    probe("archive_no_files", "POST", "/archive", [AUTH, jct], b'{"bucket":"golden"}', body_desc="no files")
    probe("archive_no_token", "POST", "/archive", [jct], b'{"bucket":"golden","files":["x.png"]}', body_desc="no token")

    # CORS
    probe("cors_preflight_upload", "OPTIONS", "/upload", [ORIGIN, ("Access-Control-Request-Method", "POST"), ("Access-Control-Request-Headers", "authorization")])
    probe("cors_preflight_delete", "OPTIONS", "/golden/x.png", [ORIGIN, ("Access-Control-Request-Method", "DELETE"), ("Access-Control-Request-Headers", "authorization")])
    probe("cors_options_without_request_method", "OPTIONS", "/upload", [ORIGIN])
    probe("cors_options_without_origin", "OPTIONS", "/upload")

    # deletes
    probe("delete_no_token", "DELETE", obj("jpg"))
    probe("delete_bad_token", "DELETE", obj("jpg"), [("Authorization", "Bearer wrong")])
    probe("delete_scoped_other", "DELETE", obj("jpg"), [SCOPED_AUTH])
    probe("delete_traversal", "DELETE", "/golden/a/../x.png", [AUTH])
    probe("delete_missing_bucket", "DELETE", "/nobucket/x.png", [AUTH])
    probe("delete_bucket_only", "DELETE", "/golden", [AUTH])
    probe("delete_ok", "DELETE", obj("jpg"), [AUTH])
    probe("get_after_delete", "GET", obj("jpg"))

    # body limit, then the metrics that everything above produced
    probe("body_too_large", "POST", "/upload", [AUTH, ("Content-Type", "multipart/form-data; boundary=" + BOUNDARY)], b"", content_length=100 * 1024 * 1024 + 1, timeout=15, body_desc="Content-Length 104857601, no body sent")
    probe("metrics_token", "GET", "/metrics", [AUTH], body_fn=metrics_body)

    # rate limits: RATE_LIMIT=5 and UPLOAD_RATE_LIMIT=3 on their own Redis db, fixed client IP
    rl = [("CF-Connecting-IP", "198.51.100.77"), AUTH]
    probe("rl_upload_url_refused", "POST", "/upload-url", rl + [jct], b'{"bucket":"golden","url":"http://127.0.0.1/a.png"}', port=RL_PORT, volatile_limits=False, body_desc="private url")
    probe("rl_index_upload_limited", "GET", "/", rl, port=RL_PORT, volatile_limits=False)
    probe("rl_health_last_allowed", "GET", "/health", rl, port=RL_PORT, volatile_limits=False)
    probe("rl_health_limited", "GET", "/health", rl, port=RL_PORT, volatile_limits=False)
    probe("rl_redis_state", "REDIS", "", redis=True)

    # DISABLE_GET, DISABLE_UPLOAD and DISABLE_DELETE all true
    for name, method, path in (("get", "GET", "/golden/x.png"), ("head", "HEAD", "/golden/x.png"), ("upload", "POST", "/upload"),
                               ("delete", "DELETE", "/golden/x.png"), ("batch_delete", "DELETE", "/batch/delete"),
                               ("batch_upload", "POST", "/batch/upload"), ("upload_url", "POST", "/upload-url"),
                               ("index", "GET", "/"), ("health", "GET", "/health"), ("resize", "POST", "/resize")):
        probe("disabled_" + name, method, path, [AUTH], port=OFF_PORT)


def cap_batch(body):
    CTX["batch"] = sorted(r["object_name"] for r in json.loads(body)["data"] if r.get("success"))


# ---------- special probes ----------

def run_websocket(p, path):
    status, hdrs, data, sock = send("GET", path, p["headers"])
    frame = "no frame"
    if sock:
        try:
            buf = data
            buf = _read_exact(sock, buf, 2)
            opcode, length = buf[0] & 0x0F, buf[1] & 0x7F
            offset = 2
            if length == 126:
                buf = _read_exact(sock, buf, 4)
                length, offset = int.from_bytes(buf[2:4], "big"), 4
            buf = _read_exact(sock, buf, offset + length)
            payload = buf[offset:offset + length]
            frame = "first frame: fin=%d opcode=%d %s" % (buf[0] >> 7, opcode, shape_body(payload))
        finally:
            sock.close()
        data = b""
    return status, hdrs, data, frame


def msgp_decode(b):
    """Only what fiber's limiter item uses: a fixmap of fixstr keys to integers."""
    out, i = [], 1
    for _ in range(b[0] & 0x0F):
        klen = b[i] & 0x1F
        key = b[i + 1:i + 1 + klen].decode()
        i += 1 + klen
        t = b[i]
        if t <= 0x7F:
            kind, val, i = "fixint", t, i + 1
        else:
            size = {0xCC: 1, 0xCD: 2, 0xCE: 4, 0xCF: 8, 0xD0: 1, 0xD1: 2, 0xD2: 4, 0xD3: 8}[t]
            kind = {0xCC: "uint8", 0xCD: "uint16", 0xCE: "uint32", 0xCF: "uint64", 0xD0: "int8", 0xD1: "int16", 0xD2: "int32", 0xD3: "int64"}[t]
            val, i = int.from_bytes(b[i + 1:i + 1 + size], "big", signed=t >= 0xD0), i + 1 + size
        out.append("%s=%s:%s" % (key, kind, "<unix-seconds>" if key == "exp" else val))
    return " ".join(out)


def run_redis():
    keys = subprocess.run(["docker", "exec", REDIS, "redis-cli", "-n", "1", "--raw", "KEYS", "*"],
                          capture_output=True, text=True, check=True).stdout.split()
    lines = ["redis db 1 (rate-limit container), keys of the probe client only"]
    for key in sorted(k for k in keys if k.startswith("198.51.100.77")):
        raw = subprocess.run(["docker", "exec", REDIS, "redis-cli", "-n", "1", "--raw", "GET", key],
                             capture_output=True, check=True).stdout.rstrip(b"\n")
        ttl = subprocess.run(["docker", "exec", REDIS, "redis-cli", "-n", "1", "TTL", key],
                             capture_output=True, text=True, check=True).stdout.strip()
        lines.append("%s -> %s (ttl %s)" % (key, msgp_decode(raw), "set" if int(ttl) > 0 else ttl))
    return "\n".join(lines) + "\n"


def main():
    build()
    if RECORD:
        os.makedirs(GOLDEN, exist_ok=True)
        for f in os.listdir(GOLDEN):
            if f.endswith(".txt"):
                os.remove(os.path.join(GOLDEN, f))
    passed = failed = 0
    for p in PROBES:
        if p.get("redis"):
            text = run_redis()
        else:
            path = p["path"]() if callable(p["path"]) else p["path"]
            body = p["body"]() if callable(p["body"]) else p["body"]
            p["shown_path"] = p.get("shown") or (path if isinstance(path, str) else path.decode("latin-1"))
            try:
                if p.get("websocket"):
                    status, hdrs, data, extra = run_websocket(p, path)
                else:
                    status, hdrs, data, _ = send(p["method"], path, p["headers"], body, p.get("port"),
                                                 p.get("content_length"), p.get("timeout", 90))
                    extra = ""
            except (OSError, ValueError, IndexError) as e:
                status, hdrs, data, extra = "NO RESPONSE (%s)" % e.__class__.__name__, [], b"", ""
            if p.get("capture") and data:
                try:
                    p["capture"](data)
                except (ValueError, KeyError, TypeError):
                    pass
            text = render(p, status, hdrs, data, extra)
        path = os.path.join(GOLDEN, p["name"] + ".txt")
        if RECORD:
            with open(path, "w", encoding="utf-8") as f:
                f.write(text)
            print("REC   " + p["name"])
            continue
        try:
            with open(path, encoding="utf-8") as f:
                want = f.read()
        except FileNotFoundError:
            want = ""
        if text == want:
            passed += 1
            print("PASS  contract " + p["name"])
        else:
            failed += 1
            print("FAIL  contract " + p["name"])
            for line in list(difflib.unified_diff(want.splitlines(), text.splitlines(), "golden", "got", lineterm=""))[:40]:
                print("      " + line)
    if RECORD:
        print("recorded %d golden files in %s" % (len(PROBES), GOLDEN))
        return 0
    print("contract: pass=%d fail=%d" % (passed, failed))
    return 1 if failed else 0


if __name__ == "__main__":
    sys.exit(main())
