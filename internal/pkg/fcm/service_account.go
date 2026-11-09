package fcm

import (
	"crypto/rsa"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/golang-jwt/jwt/v5"
)

// serviceAccountType is the "type" Google writes into a service account key file.
const serviceAccountType = "service_account"

// ErrInvalidServiceAccount says the text given is not a usable service account key.
var ErrInvalidServiceAccount = errors.New("fcm: not a usable Firebase service account key")

// ServiceAccount is the part of a Firebase service account key file push needs.
type ServiceAccount struct {
	ProjectID   string
	ClientEmail string
	privateKey  *rsa.PrivateKey
}

// serviceAccountFile is the JSON Google hands out from "Generate new private key".
type serviceAccountFile struct {
	Type        string `json:"type"`
	ProjectID   string `json:"project_id"`
	ClientEmail string `json:"client_email"`
	PrivateKey  string `json:"private_key"`
}

// ParseServiceAccount reads a service account key file.
//
// Every refusal names what is missing and never echoes the file: it holds a
// private key, and an error message is the kind of thing that ends up in a log.
func ParseServiceAccount(raw string) (ServiceAccount, error) {
	var file serviceAccountFile
	if err := json.Unmarshal([]byte(strings.TrimSpace(raw)), &file); err != nil {
		return ServiceAccount{}, fmt.Errorf("%w: it is not JSON", ErrInvalidServiceAccount)
	}
	if file.Type != "" && file.Type != serviceAccountType {
		return ServiceAccount{}, fmt.Errorf("%w: its type is %q, not %q", ErrInvalidServiceAccount, file.Type, serviceAccountType)
	}
	if missing := missingFields(file); missing != "" {
		return ServiceAccount{}, fmt.Errorf("%w: it has no %s", ErrInvalidServiceAccount, missing)
	}
	key, err := jwt.ParseRSAPrivateKeyFromPEM([]byte(file.PrivateKey))
	if err != nil {
		return ServiceAccount{}, fmt.Errorf("%w: its private_key is not an RSA key", ErrInvalidServiceAccount)
	}
	return ServiceAccount{ProjectID: file.ProjectID, ClientEmail: file.ClientEmail, privateKey: key}, nil
}

func missingFields(file serviceAccountFile) string {
	var missing []string
	if file.ProjectID == "" {
		missing = append(missing, "project_id")
	}
	if file.ClientEmail == "" {
		missing = append(missing, "client_email")
	}
	if file.PrivateKey == "" {
		missing = append(missing, "private_key")
	}
	return strings.Join(missing, ", ")
}
