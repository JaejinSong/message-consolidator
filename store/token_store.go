package store

import (
)

func SaveGmailToken(email, tokenJSON string) error {
	metadataMu.Lock()
	tokenCache[email] = tokenJSON
	metadataMu.Unlock()

	_, err := db.Exec(`
		INSERT INTO gmail_tokens (user_email, token_json, updated_at)
		VALUES ($1, $2, NOW())
		ON CONFLICT (user_email) DO UPDATE SET token_json = $2, updated_at = NOW()`,
		email, tokenJSON)
	return err
}

func GetGmailToken(email string) (string, error) {
	metadataMu.RLock()
	token, ok := tokenCache[email]
	metadataMu.RUnlock()
	if ok {
		return token, nil
	}

	var tokenJSON string
	err := db.QueryRow("SELECT token_json FROM gmail_tokens WHERE user_email = $1", email).Scan(&tokenJSON)
	if err != nil {
		return "", err
	}

	metadataMu.Lock()
	tokenCache[email] = tokenJSON
	metadataMu.Unlock()

	return tokenJSON, nil
}

func HasGmailToken(email string) bool {
	metadataMu.RLock()
	_, ok := tokenCache[email]
	metadataMu.RUnlock()
	return ok
}
