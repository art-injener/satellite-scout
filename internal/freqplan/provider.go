package freqplan

// ProviderFunc оборачивает функцию в Source (удобно для SatNOGS-адаптера снаружи).
type ProviderFunc func(noradID int) []Tx

// Transmitters — реализация Source.
func (f ProviderFunc) Transmitters(noradID int) []Tx {
	if f == nil {
		return nil
	}
	return f(noradID)
}
