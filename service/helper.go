package service

import (
	"fmt"
	"log"
	"math/rand"
	"time"

	"github.com/joho/godotenv"
)

func EnvLoad(){
	err := godotenv.Load()
	if err != nil {
		log.Fatal("Error loading .env file")
	}
}

func RandomName(length int) string{
	rand.Seed(time.Now().UnixNano())
	b := make([]byte, length)
	rand.Read(b)
	return fmt.Sprintf("%x", b)[:length]
}