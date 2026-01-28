package linear

import "github.com/zalando/go-keyring"

const tokenServiceName = "amux.linear"

// LoadToken retrieves a stored OAuth token from the OS keychain.
func LoadToken(account string) (string, error) {
	if account == "" {
		return "", keyring.ErrNotFound
	}
	return keyring.Get(tokenServiceName, account)
}

// StoreToken persists an OAuth token into the OS keychain.
func StoreToken(account, token string) error {
	if account == "" {
		return keyring.ErrNotFound
	}
	if token == "" {
		return keyring.ErrNotFound
	}
	return keyring.Set(tokenServiceName, account, token)
}

// DeleteToken removes a token from the OS keychain.
func DeleteToken(account string) error {
	if account == "" {
		return keyring.ErrNotFound
	}
	return keyring.Delete(tokenServiceName, account)
}
