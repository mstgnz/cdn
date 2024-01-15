package service

import (
	"io"
	"reflect"
	"strconv"
	"testing"
)

func TestDownloadFile(t *testing.T) {
	type args struct {
		filepath string
		url      string
	}
	tests := []struct {
		args    args
		wantErr bool
	}{
		// TODO: Add test cases.
		{},
	}
	for i, tt := range tests {
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			if err := DownloadFile(tt.args.filepath, tt.args.url); (err != nil) != tt.wantErr {
				t.Errorf("DownloadFile() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestGetBool(t *testing.T) {
	type args struct {
		key string
	}
	tests := []struct {
		args args
		want bool
	}{
		// TODO: Add test cases.
		{},
	}
	for i, tt := range tests {
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			if got := GetBool(tt.args.key); got != tt.want {
				t.Errorf("GetBool() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGetEnv(t *testing.T) {
	type args struct {
		key string
	}
	tests := []struct {
		args args
		want string
	}{
		// TODO: Add test cases.
		{},
	}
	for i, tt := range tests {
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			if got := GetEnv(tt.args.key); got != tt.want {
				t.Errorf("GetEnv() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestImageToByte(t *testing.T) {
	type args struct {
		img string
	}
	tests := []struct {
		args args
		want []byte
	}{
		// TODO: Add test cases.
		{},
	}
	for i, tt := range tests {
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			if got := ImageToByte(tt.args.img); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ImageToByte() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsInt(t *testing.T) {
	type args struct {
		one string
		two string
	}
	tests := []struct {
		args args
		want bool
	}{
		// TODO: Add test cases.
		{},
	}
	for i, tt := range tests {
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			if got := IsInt(tt.args.one, tt.args.two); got != tt.want {
				t.Errorf("IsInt() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestRandomName(t *testing.T) {
	type args struct {
		length int
	}
	tests := []struct {
		args args
		want string
	}{
		// TODO: Add test cases.
		{},
	}
	for i, tt := range tests {
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			if got := RandomName(tt.args.length); got != tt.want {
				t.Errorf("RandomName() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSetWidthToHeight(t *testing.T) {
	type args struct {
		width  string
		height string
	}
	tests := []struct {
		args  args
		want  string
		want1 string
	}{
		// TODO: Add test cases.
		{},
	}
	for i, tt := range tests {
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			got, got1 := SetWidthToHeight(tt.args.width, tt.args.height)
			if got != tt.want {
				t.Errorf("SetWidthToHeight() got = %v, want %v", got, tt.want)
			}
			if got1 != tt.want1 {
				t.Errorf("SetWidthToHeight() got1 = %v, want %v", got1, tt.want1)
			}
		})
	}
}

func TestStreamToByte(t *testing.T) {
	type args struct {
		stream io.Reader
	}
	tests := []struct {
		args args
		want []byte
	}{
		// TODO: Add test cases.
		{},
	}
	for i, tt := range tests {
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			if got := StreamToByte(tt.args.stream); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("StreamToByte() = %v, want %v", got, tt.want)
			}
		})
	}
}
