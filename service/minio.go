package service

import (
	"log"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

func MinioClient() *minio.Client{

	endpoint := GetEnv("MINIO_ENDPOINT")
	accessKey := GetEnv("MINIO_ROOT_USER")
	secretKey := GetEnv("MINIO_ROOT_PASSWORD")

	minioClient, err := minio.New(endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(accessKey, secretKey, ""),
		Secure: false,
	})
	if err != nil {
		log.Fatalln(err)
	}

	log.Printf("%#v\n", minioClient)

	return minioClient
}