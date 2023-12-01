package sdk

type ClientI interface {
	GetConfig() ClientConfig
}

type Client struct {
	config ClientConfig
}

func (c Client) GetConfig() ClientConfig {
	return c.config
}

func CreateDefaultClient(config ClientConfig) Client {
	return Client{config: config}
}

func CreateClient(config ClientConfig, _ HTTPClient) Client {
	return Client{config: config}
}
