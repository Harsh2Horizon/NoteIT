package auth

import "testing"

func TestPasswordHash(t *testing.T) {
	password := "a sufficiently long password"
	hash, err := HashPassword(password)
	if err != nil {
		t.Fatal(err)
	}
	if !CheckPassword(hash, password) || CheckPassword(hash, "wrong password") {
		t.Fatal("password verification failed")
	}
	other, err := HashPassword(password)
	if err != nil {
		t.Fatal(err)
	}
	if hash == other {
		t.Fatal("passwords must have independent salts")
	}
	for _, invalid := range []string{"", "pbkdf2-sha256$999999999999$x$y", "pbkdf2-sha256$600000$xx$yy"} {
		if CheckPassword(invalid, password) {
			t.Fatal("accepted invalid hash")
		}
	}
}
