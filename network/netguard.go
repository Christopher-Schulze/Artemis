package network

import "net/url"

// Netguard blocks private targets before navigation (pinchtab netguard floor).
type Netguard struct {
	BlockPrivate bool
}

func (n Netguard) Allow(rawURL string) error {
	if !n.BlockPrivate {
		return nil
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return err
	}
	return CheckHostPublic(u)
}
