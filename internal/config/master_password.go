package config

import (
	"crypto/rand"
	"fmt"
)

// EnsureMasterPassword генерирует пароль мастера, если в конфиге он пуст, и сохраняет его
// в файл, с которым запущено хранилище. Возвращает true, если пароль сгенерирован.
// Ошибка записи возвращается: без сохранённого пароля удалённый мастер не сможет войти.
func EnsureMasterPassword(store *Store) (bool, error) {
	cfg := store.Get()
	if cfg == nil {
		return false, ErrConfigStoreEmpty
	}
	if cfg.Server.MasterPassword != "" {
		return false, nil
	}
	password := rand.Text()
	err := store.Update(func(c *Config) error {
		c.Server.MasterPassword = password
		return nil
	})
	if err != nil {
		return false, fmt.Errorf("save generated master password to %s: %w", store.Path(), err)
	}
	return true, nil
}
