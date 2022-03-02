## Cdn Api With Go and MinIO
I am developing cdn api service with golang, minio and docker. We also use it at the company I work for. All processes on Minio will be added as api service over time.
I am developing cdn api service with golang, minio and docker. We also use it at the company I work for. All processes on Minio will be added as api service over time.


### Image Upload

POST: http://localhost:9090/upload

NOTE: Every file is uploaded to the glacier and the minio. Since we use minio on our server, we back up our files uploaded on minio with glacier.

| KEY    | VALUE       |
|--------|-------------|
| bucket | bucket name |
| path   | slider      |
| file   | choose file |

RESULT :

```
{
    "error": false,
    "awsResult": {
        "ArchiveId": "R4cAhSVAwAm6DwcWP51r95eUXZF",
        "Checksum": "cbf9062f4ff5269d479c47680bcf",
        "Location": "/00000000000/vaults/vaultname/archives/R4cAhSVAwA",
        "ResultMetadata": {}
    },
    "awsUpload": "Glacier Successfully Uploaded",
    "minioResult": {
        "Bucket": "bucketname",
        "Key": "aws/0f051ba2e4.jpeg",
        "ETag": "5e4d41ad71e7e3406bee79875e96909b",
        "Size": 26357,
        "LastModified": "0001-01-01T00:00:00Z",
        "Location": "",
        "VersionID": "",
        "Expiration": "0001-01-01T00:00:00Z",
        "ExpirationRuleID": ""
    },
    "minioUpload": "Successfully Uploaded localhost:9090/bucketname/aws/0f051ba2e4.jpeg of size 26357"
}
```

### Image Get
GET IMAGE : http://localhost:9090/bucket-name/object-name  
GET IMAGE WIDTH SIZE : http://localhost:9090/bucketname/w:300/h:250/object-name  
GET IMAGE WIDTH WITH : http://localhost:9090/bucketname/w:300/object-name  
GET IMAGE WIDTH HEIGHT : http://localhost:9090/bucketname/h:300/object-name

### Image Delete

DELETE: http://localhost:9090/bucket-name/object-name  
WIDTH: Authorization


### SOURCE

[minio golang sdk](https://docs.min.io/docs/golang-client-api-reference.html)  
[imagemagick releases](https://download.imagemagick.org/ImageMagick/download/releases/)

[aws-s3-glacier](https://docs.aws.amazon.com/amazonglacier/latest/dev/introduction.html)  
[aws-cli-glacier](README.md)