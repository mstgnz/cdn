## Cdn Api With Go and MinIO
I am developing cdn api service with golang, minio and docker. We also use it at the company I work for. All processes on Minio will be added as api service over time.
I am developing cdn api service with golang, minio and docker. We also use it at the company I work for. All processes on Minio will be added as api service over time.


### Image Upload

POST: http://localhost:9090/upload

| KEY    | VALUE       |
|--------|-------------|
| bucket | bucket name |
| path   | slider      |
| file   | choose file |

RESULT :

```
{
    "error": false,
    "info": {
        "Bucket": "destech",
        "Key": "slider/9561aae44a.jpeg",
        "ETag": "5e4d41ad71e7e3406bee79875e96909b",
        "Size": 26357,
        "LastModified": "0001-01-01T00:00:00Z",
        "Location": "",
        "VersionID": "",
        "Expiration": "0001-01-01T00:00:00Z",
        "ExpirationRuleID": ""
    },
    "msg": "Successfully uploaded slider/9561aae44a.jpeg of size 26357"
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
