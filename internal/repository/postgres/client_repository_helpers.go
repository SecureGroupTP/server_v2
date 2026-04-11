package postgres

func containsState(values []int16, target int16) bool {
	for _, item := range values {
		if item == target {
			return true
		}
	}
	return false
}

func pqByteaArray(values [][]byte) [][]byte {
	return values
}
