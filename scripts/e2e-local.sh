#!/bin/bash
# End-to-end check of the cdn:ci image against its own MinIO and Redis, with
# generated credentials and no AWS settings, so nothing reaches a real account
# and the local .env is never read. Run with `make e2e` after `make ci`.
# Needs Docker, curl, openssl and python3. Works with the bash 3.2 macOS ships.
set -u
cd "$(dirname "$0")/.."

PORT=${E2E_PORT:-19090}
MINIO_IMAGE=${E2E_MINIO_IMAGE:-quay.io/minio/minio:RELEASE.2022-10-24T18-35-07Z}
B=http://127.0.0.1:$PORT
P=cdne2e
W=$(mktemp -d)
pass=0; fail=0
ok()  { echo "PASS  $*"; pass=$((pass+1)); }
bad() { echo "FAIL  $*"; fail=$((fail+1)); }

teardown() {
  docker rm -f $P-api $P-redis $P-minio >/dev/null 2>&1
  docker network rm $P >/dev/null 2>&1
}
trap 'teardown; rm -rf "$W"' EXIT
teardown # leftovers from an interrupted run

TOKEN=$(openssl rand -hex 32)
MU=e2euser
MP=$(openssl rand -hex 16)
cat > "$W/test.env" <<EOF
TOKEN=$TOKEN
APP_PORT=9090
APP_URL=http://localhost:$PORT
MINIO_ENDPOINT=$P-minio:9000
MINIO_ROOT_USER=$MU
MINIO_ROOT_PASSWORD=$MP
MINIO_USE_SSL=false
REDIS_URL=redis://$P-redis:6379
VALIDATE_FILE=true
RATE_LIMIT=1000
UPLOAD_RATE_LIMIT=1000
EOF

echo "### stack"
docker network create $P >/dev/null
docker run -d --name $P-minio --network $P -e MINIO_ROOT_USER=$MU -e MINIO_ROOT_PASSWORD=$MP "$MINIO_IMAGE" server /data >/dev/null
docker run -d --name $P-redis --network $P redis:7.2-alpine >/dev/null
sleep 3
docker run -d --name $P-api --network $P --env-file "$W/test.env" -p 127.0.0.1:$PORT:9090 cdn:ci >/dev/null
for _ in $(seq 1 40); do
  [ "$(curl -s -o /dev/null -w '%{http_code}' $B/health)" = 200 ] && break; sleep 1
done
if [ "$(curl -s -o /dev/null -w '%{http_code}' $B/health)" = 200 ]; then ok "health 200 after boot"
else bad "health never reached 200"; docker logs $P-api 2>&1 | tail -30; exit 1; fi

echo "### fixtures"
mkdir -p "$W/fx"
docker run --rm -v "$W/fx":/fx --entrypoint sh cdn:ci -c \
  'for f in jpg png webp gif tiff; do magick -size 400x300 gradient:red-blue "/fx/t.$f"; done'
printf '%%PDF-1.4\n1 0 obj << /Type /Catalog >> endobj\ntrailer << /Root 1 0 R >>\n%%%%EOF\n' > "$W/fx/t.pdf"
# ISO base media header as iPhones write it; the content check reads the box, not pixels.
{ printf '\x00\x00\x00\x18ftypheic\x00\x00\x00\x00mif1heic'; head -c 2048 /dev/urandom; } > "$W/fx/t.heic"
printf '<?php echo 1; ?>' > "$W/fx/t.php"

auth=(-H "Authorization: Bearer $TOKEN")
setlink() { echo "$2" > "$W/link.$1"; }
getlink() { cat "$W/link.$1" 2>/dev/null; }
width() { docker run --rm -v "$W":/w --entrypoint magick cdn:ci identify -format '%w' "/w/$1" 2>/dev/null; }
json() { python3 -c "import json,sys; d=json.load(sys.stdin); $1" 2>/dev/null; }

echo "### upload, 201 expected (a consumer rejects anything else)"
for f in jpg png webp gif tiff pdf heic; do
  out=$(curl -s -w '\n%{http_code}' "${auth[@]}" -F "file=@$W/fx/t.$f" -F bucket=e2e -F path=single $B/upload)
  code=$(echo "$out" | tail -1); body=$(echo "$out" | sed '$d')
  link=$(echo "$body" | json 'print(d.get("data",{}).get("link",""))')
  if [ "$code" = 201 ] && [ -n "$link" ]; then ok "upload .$f"; setlink $f "${link#http://localhost:$PORT}"
  else bad "upload .$f code=$code body=$(echo "$body" | head -c 200)"; fi
done

echo "### read back byte for byte"
for f in jpg png webp gif tiff pdf heic; do
  [ -n "$(getlink $f)" ] || continue
  curl -s -o "$W/got.$f" "$B$(getlink $f)"
  cmp -s "$W/got.$f" "$W/fx/t.$f" && ok "get .$f identical" || bad "get .$f differs"
done

echo "### resize on read"
curl -s -o "$W/rs.path" "$B$(getlink jpg | sed 's#^/e2e/#/e2e/w:100/#')"
[ "$(width rs.path)" = 100 ] && ok "path form w:100" || bad "path form width=$(width rs.path)"
curl -s -o "$W/rs.query" "$B$(getlink jpg)?width=100"
[ "$(width rs.query)" = 100 ] && ok "query form ?width=100" || bad "query form width=$(width rs.query)"
for f in png webp gif tiff; do
  curl -s -o "$W/rs.$f" "$B$(getlink $f)?width=80"
  [ "$(width "rs.$f[0]")" = 80 ] && ok "resize .$f" || bad "resize .$f width=$(width "rs.$f[0]")"
done

echo "### POST /resize answers with the image"
meta=$(curl -s -o "$W/post-resize" -w '%{http_code} %{content_type}' "${auth[@]}" -F "file=@$W/fx/t.png" -F width=50 $B/resize)
[ "$(width post-resize)" = 50 ] && ok "POST /resize ($meta)" || bad "POST /resize $meta width=$(width post-resize)"

echo "### batch upload (field name is files)"
out=$(curl -s "${auth[@]}" -F "files=@$W/fx/t.jpg" -F "files=@$W/fx/t.png" -F "files=@$W/fx/t.webp" -F bucket=e2e -F path=batch $B/batch/upload)
names=$(echo "$out" | json 'print(" ".join(i["object_name"] for i in d["data"] if i.get("success")))')
n=$(echo $names | wc -w | tr -d ' ')
[ "$n" = 3 ] && ok "batch upload 3/3" || bad "batch upload $n/3: $(echo "$out" | head -c 300)"
for o in $names; do
  c=$(curl -s -o /dev/null -w '%{http_code}' "$B/e2e/$o"); [ "$c" = 200 ] || bad "batch object $o GET $c"
done

echo "### delete (a missing object serves the placeholder with 200, by design)"
curl -s -o "$W/placeholder" "$B/e2e/does/not/exist.jpg"
c=$(curl -s -o /dev/null -w '%{http_code}' -X DELETE "${auth[@]}" "$B$(getlink jpg)")
[ "$c" = 200 ] && ok "delete" || bad "delete $c"
curl -s -o "$W/after-delete" "$B$(getlink jpg)"
cmp -s "$W/after-delete" "$W/placeholder" && ok "deleted object serves the placeholder" || bad "deleted object still served"

echo "### batch delete"
files_json=$(python3 -c 'import json,sys; print(json.dumps(sys.argv[1:]))' $names)
out=$(curl -s -X DELETE "${auth[@]}" -H 'Content-Type: application/json' -d "{\"bucket\":\"e2e\",\"files\":$files_json}" $B/batch/delete)
okn=$(echo "$out" | json 'print(sum(1 for i in d["data"] if i.get("success")))')
[ "$okn" = 3 ] && ok "batch delete 3/3" || bad "batch delete: $(echo "$out" | head -c 300)"

echo "### refusals"
c=$(curl -s -o /dev/null -w '%{http_code}' "${auth[@]}" -F "file=@$W/fx/t.php" -F bucket=e2e $B/upload)
[ "${c:0:1}" = 4 ] && ok ".php refused ($c)" || bad ".php got $c"
# Auth failures answer 400, not 401, deliberately: see BucketAuthMiddleware.
out=$(curl -s -w '\n%{http_code}' -F "file=@$W/fx/t.png" -F bucket=e2e $B/upload)
[ "$(echo "$out" | tail -1)" = 400 ] && echo "$out" | grep -q 'no token provided' \
  && ok "upload without token refused" || bad "upload without token: $(echo "$out" | tr '\n' ' ')"

echo "### after"
[ "$(curl -s -o /dev/null -w '%{http_code}' $B/health)" = 200 ] && ok "health still 200" || bad "health not 200"
if docker logs $P-api 2>&1 | grep -iqE 'panic|segfault|error while loading'; then
  bad "api log shows a crash"; docker logs $P-api 2>&1 | grep -iE 'panic|segfault|error while loading' | head -5
else ok "no panic or loader error in the api log"; fi

echo
echo "e2e: pass=$pass fail=$fail"
[ "$fail" = 0 ]
