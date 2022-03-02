## Glacier With Aws Cli

Link: [cli-services-glacier](https://docs.aws.amazon.com/cli/latest/reference/glacier/index.html)

#### 1) create a vault from the command line
```bash
aws glacier create-vault --account-id - --vault-name vaultName
```

#### 2) Create a file named 3mb large file
```bash
dd if=/dev/urandom of=largefile bs=3145728 count=1
```

#### 3) split file into chunks
```bash
split --bytes=1048576 --verbose largefile chunk
```

#### 4) We open a space for multipart upload
```bash
aws glacier initiate-multipart-upload --account-id - --archive-description "multipart upload test" --part-size 1048576 --vault-name vaultName
```
result
```json
{
    "location": "/097479760550/vaults/vaultName/multipart-uploads/ssW0KxfMgdJ4LTRposuVTpPgrGsTAPze_GQUwtFWrJwIKEkNOdX_F0z-yNAA_pQIptvpZueGWWUszGZ446HBIE9aWWVr",
    "uploadId": "ssW0KxfMgdJ4LTRposuVTpPgrGsTAPze_GQUwtFWrJwIKEkNOdX_F0z-yNAA_pQIptvpZueGWWUszGZ446HBIE9aWWVr"
}
```

#### 5) Then we assign it to a variable and start multipart upload
```bash
$UPLOADID="ssW0KxfMgdJ4LTRposuVTpPgrGsTAPze_GQUwtFWrJwIKEkNOdX_F0z-yNAA_pQIptvpZueGWWUszGZ446HBIE9aWWVr"
```
```bash
aws glacier upload-multipart-part --upload-id $UPLOADID --body chunkaa --range 'bytes 0-1048575/*' --account-id - --vault-name vaultName
aws glacier upload-multipart-part --upload-id $UPLOADID --body chunkab --range 'bytes 1048576-2097151/*' --account-id - --vault-name vaultName
aws glacier upload-multipart-part --upload-id $UPLOADID --body chunkac --range 'bytes 2097152-3145727/*' --account-id - --vault-name vaultName
```

#### 6) We are hashing the file with openssl so that we can check if the file here and the outgoing file are the same.
```bash
openssl dgst -sha256 -binary chunkaa > hash1
openssl dgst -sha256 -binary chunkab > hash2
openssl dgst -sha256 -binary chunkac > hash3
```

#### 7) We make a new file with the first 2 hashes and then we get its hash as well.
```bash
cat hash1 hash2 > hash12
openssl dgst -sha256 -binary hash12 > hash12hash
```

#### 8) then we combine it with the third file and finally we assign that hash value to the variable
```bash
cat hash12hash hash3 > hash123
openssl dgst -sha256 hash123
SHA256(hash123)=9628195fcdbcbbe76cdde932d4646fa7de5f219fb39823836d81f0cc0e18aa67
TREEHASH=9628195fcdbcbbe76cdde932d4646fa7de5f219fb39823836d81f0cc0e18aa67
```

#### 9) Finally, we complete the upload
```bash
aws glacier complete-multipart-upload --checksum $TREEHASH --archive-size 3145728 --upload-id $UPLOADID --account-id - --vault-name vaultName
```
