package service

import (
	"reflect"
	"strconv"
	"testing"
)

func TestImagickResize(t *testing.T) {
	type args struct {
		image   []byte
		hWidth  uint
		hHeight uint
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
			if got := ImagickResize(tt.args.image, tt.args.hWidth, tt.args.hHeight); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ImagickResize() = %v, want %v", got, tt.want)
			}
		})
	}
}
