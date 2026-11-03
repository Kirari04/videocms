package auth

import (
	"ch/kirari04/videocms/models"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"gorm.io/gorm"
)

func TestLogoutRevokesOnlyItsSessionIncludingRefreshedTokens(t *testing.T) {
	for _, legacy := range []bool{false, true} {
		t.Run(map[bool]string{false: "new session", true: "legacy session"}[legacy], func(t *testing.T) {
			s, user := newJWTTestService(t)
			first, _, err := s.GenerateTimeJWT(user, time.Now().Add(2*time.Minute))
			if err != nil {
				t.Fatal(err)
			}
			if legacy {
				first = signSessionClaims(t, s, jwt.SigningMethodHS256, jwt.MapClaims{"userid": user.ID, "exp": time.Now().Add(2 * time.Minute).Unix()})
			}
			other, _, err := s.GenerateJWT(user)
			if err != nil {
				t.Fatal(err)
			}
			refreshed, _, err := s.RefreshJWT(first)
			if err != nil {
				t.Fatal(err)
			}
			_, claims, err := s.VerifyJWT(refreshed)
			if err != nil || claims.ID == "" {
				t.Fatal("refreshed session has no identifier")
			}
			if err := s.RevokeJWT(first); err != nil {
				t.Fatal(err)
			}
			// A fresh service exercises durable state, rather than an in-memory list.
			s = NewService(s.Deps)
			for _, token := range []string{first, refreshed} {
				if _, _, err := s.VerifyJWT(token); err == nil {
					t.Fatal("logged-out session accepted")
				}
				if _, _, err := s.RefreshJWT(token); err == nil {
					t.Fatal("logged-out session refreshed")
				}
			}
			if _, _, err := s.VerifyJWT(other); err != nil {
				t.Fatalf("unrelated session revoked: %v", err)
			}
			var current models.User
			if err := s.Deps.DB.First(&current, user.ID).Error; err != nil || current.SessionVersion != user.SessionVersion {
				t.Fatal("logout changed account-wide version")
			}
		})
	}
}

func TestRefreshCannotSurviveConcurrentSigningKeyRotation(t *testing.T) {
	for _, legacy := range []bool{false, true} {
		t.Run(map[bool]string{false: "new session", true: "legacy session"}[legacy], func(t *testing.T) {
			s, user := newJWTTestService(t)
			token, _, err := s.GenerateTimeJWT(user, time.Now().Add(2*time.Minute))
			if err != nil {
				t.Fatal(err)
			}
			if legacy {
				token = signSessionClaims(t, s, jwt.SigningMethodHS256, jwt.MapClaims{"userid": user.ID, "exp": time.Now().Add(2 * time.Minute).Unix()})
			}
			verified := make(chan struct{})
			resume := make(chan struct{})
			var once sync.Once
			if err := s.Deps.DB.Callback().Query().After("gorm:query").Register("test:rotate_during_refresh", func(tx *gorm.DB) {
				if tx.Statement.Table == "revoked_auth_sessions" {
					once.Do(func() { close(verified); <-resume })
				}
			}); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = s.Deps.DB.Callback().Query().Remove("test:rotate_during_refresh") })
			result := make(chan struct {
				token string
				err   error
			}, 1)
			go func() {
				refreshed, _, err := s.RefreshJWT(token)
				result <- struct {
					token string
					err   error
				}{refreshed, err}
			}()
			select {
			case <-verified:
			case <-time.After(5 * time.Second):
				close(resume)
				t.Fatal("refresh did not reach the verification boundary")
			}
			snapshot := s.Deps.Snapshots.Current()
			snapshot.Config.JwtSecretKey = "new-key-published-by-rotation"
			s.Deps.Snapshots.Replace(snapshot)
			close(resume)
			var refreshed string
			select {
			case value := <-result:
				if value.err != nil {
					t.Fatal(value.err)
				}
				refreshed = value.token
			case <-time.After(5 * time.Second):
				t.Fatal("refresh did not complete")
			}
			for _, value := range []string{token, refreshed} {
				if _, _, err := s.VerifyJWT(value); err == nil {
					t.Fatal("old session survived concurrent signing-key rotation")
				}
			}
			fresh, _, err := s.GenerateJWT(user)
			if err != nil {
				t.Fatal(err)
			}
			if _, _, err := s.VerifyJWT(fresh); err != nil {
				t.Fatalf("fresh login rejected after rotation: %v", err)
			}
		})
	}
}

func TestLegacyLogoutRejectsEquivalentSignatureEncodings(t *testing.T) {
	s, user := newJWTTestService(t)
	token := signSessionClaims(t, s, jwt.SigningMethodHS256, jwt.MapClaims{"userid": user.ID, "exp": time.Now().Add(2 * time.Minute).Unix()})
	const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_"
	index := strings.IndexByte(alphabet, token[len(token)-1])
	if index < 0 || index%4 != 0 {
		t.Fatal("unexpected canonical HS256 signature encoding")
	}
	alias := token[:len(token)-1] + string(alphabet[index+1])
	if _, _, err := s.VerifyJWT(alias); err != nil {
		t.Fatalf("equivalent signature encoding rejected before logout: %v", err)
	}
	refreshed, _, err := s.RefreshJWT(alias)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.RevokeJWT(token); err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{token, alias, refreshed} {
		if _, _, err := s.VerifyJWT(value); err == nil {
			t.Fatal("legacy session accepted after logout")
		}
		if _, _, err := s.RefreshJWT(value); err == nil {
			t.Fatal("legacy session refreshed after logout")
		}
	}
}
