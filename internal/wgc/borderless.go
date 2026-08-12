package wgc

import "fmt"

const borderlessAccessAllowed = int32(4)

func requireBorderlessAccess(status int32) error {
	if status != borderlessAccessAllowed {
		return fmt.Errorf("borderless capture access was not allowed: status=%d", status)
	}
	return nil
}

func requireBorderSetting(required, actual bool) error {
	if actual != required {
		return fmt.Errorf("IsBorderRequired verification failed: requested=%t actual=%t", required, actual)
	}
	return nil
}
