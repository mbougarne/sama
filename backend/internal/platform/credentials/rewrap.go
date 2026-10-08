package credentials

// Rewrap changes only the master-key envelope. The credential ciphertext,
// nonce, format and authenticated identity remain unchanged.
func (k *Keyring) Rewrap(binding Binding, envelope Envelope, target string) (Envelope, error) {
	if k.Require([]string{envelope.KeyID, target}) != nil {
		return Envelope{}, ErrKeyring
	}
	aad, err := binding.aad("data-key")
	if err != nil || envelope.Format != 1 || len(envelope.WrappedKey) != 48 {
		return Envelope{}, ErrEnvelope
	}
	dataKey, err := open(k.keys[envelope.KeyID], envelope.WrappedKey, envelope.WrappingNonce, aad)
	if err != nil {
		return Envelope{}, ErrEnvelope
	}
	defer clear(dataKey)
	wrapped, nonce, err := seal(k.keys[target], dataKey, aad)
	if err != nil {
		return Envelope{}, ErrEnvelope
	}
	envelope.WrappedKey = wrapped
	envelope.WrappingNonce = nonce
	envelope.KeyID = target
	return envelope, nil
}
