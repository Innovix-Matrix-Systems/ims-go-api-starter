package config

// Mail driver settings (MAIL_*).

// Mail holds the mail driver settings.
type Mail struct {
	Driver      string
	Host        string
	Port        int
	Username    string
	Password    string
	Encryption  string
	FromAddress string
	FromName    string
}

func (c *Config) loadMail() error {
	port, err := getInt("MAIL_PORT", 2525)
	if err != nil {
		return err
	}
	c.Mail = Mail{
		Driver:      getString("MAIL_DRIVER", "log"),
		Host:        getString("MAIL_HOST", "127.0.0.1"),
		Port:        port,
		Username:    getString("MAIL_USERNAME", ""),
		Password:    getString("MAIL_PASSWORD", ""),
		Encryption:  getString("MAIL_ENCRYPTION", ""),
		FromAddress: getString("MAIL_FROM_ADDRESS", "hello@example.com"),
		FromName:    getString("MAIL_FROM_NAME", c.App.Name),
	}
	return nil
}
