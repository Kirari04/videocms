package auth

import (
	"testing"
	"time"

	"ch/kirari04/videocms/models"
	"github.com/golang-jwt/jwt/v5"
)

func TestFolderShareTokensAreScopedAndExpire(t *testing.T) {
	s := newTestService("media-secret")
	expires := time.Now().Add(time.Minute)
	share := &models.FolderShare{Token: "random-share-token", AccessVersion: "version", ExpiresAt: &expires}
	token, expiry, err := s.GenerateFolderShareToken(share)
	if err != nil || !expiry.Equal(expires) || !s.VerifyFolderShareToken(token, share) {
		t.Fatalf("invalid share session: %v, %v", expiry, err)
	}
	if _, _, err := s.VerifyJWT(token); err == nil {
		t.Fatal("visitor session was accepted as an account JWT")
	}
	other := *share
	other.Token = "different-share"
	if s.VerifyFolderShareToken(token, &other) {
		t.Fatal("visitor session unlocked another share")
	}
	other = *share
	other.AccessVersion = "changed"
	if s.VerifyFolderShareToken(token, &other) {
		t.Fatal("visitor session survived a settings change")
	}
	for _, test := range []struct {
		name     string
		method   jwt.SigningMethod
		audience string
		expires  *jwt.NumericDate
	}{
		{"expired", jwt.SigningMethodHS256, folderShareAudience, jwt.NewNumericDate(time.Now().Add(-time.Minute))},
		{"no expiry", jwt.SigningMethodHS256, folderShareAudience, nil},
		{"wrong audience", jwt.SigningMethodHS256, "account", jwt.NewNumericDate(expires)},
		{"wrong algorithm", jwt.SigningMethodHS384, folderShareAudience, jwt.NewNumericDate(expires)},
	} {
		t.Run(test.name, func(t *testing.T) {
			claims := FolderShareClaims{AccessVersion: share.AccessVersion, RegisteredClaims: jwt.RegisteredClaims{
				Subject: share.Token, Audience: jwt.ClaimStrings{test.audience}, ExpiresAt: test.expires,
			}}
			forged, err := jwt.NewWithClaims(test.method, claims).SignedString([]byte(s.Config().JwtSecretKey + "_folder_share"))
			if err != nil {
				t.Fatal(err)
			}
			if s.VerifyFolderShareToken(forged, share) {
				t.Fatal("invalid visitor session accepted")
			}
		})
	}
}
