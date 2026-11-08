package destination

import "testing"

func TestProductionS3RequiresTLS(t *testing.T) {
	t.Setenv("AIE_ENV", "production")
	for _, opts := range []S3Options{{Endpoint: "http://backup.example", UseSSL: true}, {Endpoint: "backup.example", UseSSL: false}} {
		if _, err := NewS3Destination(opts); err == nil {
			t.Fatal("plaintext backup accepted")
		}
	}
	if _, err := NewS3Destination(S3Options{Endpoint: "https://backup.example"}); err != nil {
		t.Fatal(err)
	}
}
