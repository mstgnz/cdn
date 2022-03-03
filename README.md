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
    "awsResult": {
        "Location": "https://test.s3.eu-central-1.amazonaws.com/aws/5e60323f7f.jpeg",
        "UploadID": "",
        "CompletedParts": null,
        "BucketKeyEnabled": false,
        "ChecksumCRC32": null,
        "ChecksumCRC32C": null,
        "ChecksumSHA1": null,
        "ChecksumSHA256": null,
        "ETag": "\"5e4d41ad71e7e3406bee79875e96909b\"",
        "Expiration": null,
        "Key": "aws/5e60323f7f.jpeg",
        "RequestCharged": "",
        "SSEKMSKeyId": null,
        "ServerSideEncryption": "",
        "VersionID": null
    },
    "awsUpload": "S3 Successfully Uploaded",
    "error": false,
    "minioResult": {
        "Bucket": "bucketname",
        "Key": "aws/5e60323f7f.jpeg",
        "ETag": "5e4d41ad71e7e3406bee79875e96909b",
        "Size": 26357,
        "LastModified": "0001-01-01T00:00:00Z",
        "Location": "",
        "VersionID": "",
        "Expiration": "0001-01-01T00:00:00Z",
        "ExpirationRuleID": ""
    },
    "minioUpload": "Minio Successfully Uploaded localhost:9090/bucketname/aws/5e60323f7f.jpeg of size 26357"
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

[go s3 pkg](https://pkg.go.dev/github.com/aws/aws-sdk-go-v2/service/s3)

[aws storage service](https://docs.aws.amazon.com/AmazonS3/latest/userguide/storage-class-intro.html)

[minio golang sdk](https://docs.min.io/docs/golang-client-api-reference.html)  
[imagemagick releases](https://download.imagemagick.org/ImageMagick/download/releases/)

[aws-s3-glacier](https://docs.aws.amazon.com/amazonglacier/latest/dev/introduction.html)  
[aws-cli-glacier](README.md)