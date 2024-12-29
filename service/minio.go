package service

import (
	"log"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"github.com/mstgnz/cdn/pkg/config"
)

func MinioClient() *minio.Client {

	endpoint := config.GetEnvOrDefault("MINIO_ENDPOINT", "localhost:9000")
	accessKey := config.GetEnvOrDefault("MINIO_ROOT_USER", "minioadmin")
	secretKey := config.GetEnvOrDefault("MINIO_ROOT_PASSWORD", "minioadmin")

	minioClient, err := minio.New(endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(accessKey, secretKey, ""),
		Secure: false,
	})
	if err != nil {
		log.Fatalln("MINIO CLIENT ERROR: ", err)
	}

	log.Printf("%#v\n", minioClient)

	return minioClient
}
