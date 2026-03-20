package handlers

import (
	"message-consolidator/config"
)

var cfg *config.Config

func Init(c *config.Config) {
	cfg = c
}
