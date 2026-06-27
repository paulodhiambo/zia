package moneyutil

import (
	"fmt"
	"math"
)

func ToMinor(amount float64, currency string) int64 {
	switch currency {
	case "KES", "NGN", "GHS", "UGX", "TZS", "RWF":
		return int64(math.Round(amount * 100))
	case "USD", "EUR", "GBP":
		return int64(math.Round(amount * 100))
	case "JPY":
		return int64(math.Round(amount))
	default:
		return int64(math.Round(amount * 100))
	}
}

func ToMajor(amountMinor int64, currency string) float64 {
	switch currency {
	case "KES", "NGN", "GHS", "UGX", "TZS", "RWF":
		return float64(amountMinor) / 100
	case "USD", "EUR", "GBP":
		return float64(amountMinor) / 100
	case "JPY":
		return float64(amountMinor)
	default:
		return float64(amountMinor) / 100
	}
}

func FormatMinor(amountMinor int64, currency string) string {
	major := ToMajor(amountMinor, currency)
	switch currency {
	case "KES", "UGX", "TZS", "RWF":
		return fmt.Sprintf("KES %.2f", major)
	case "USD":
		return fmt.Sprintf("$%.2f", major)
	case "EUR":
		return fmt.Sprintf("€%.2f", major)
	case "GBP":
		return fmt.Sprintf("£%.2f", major)
	case "NGN":
		return fmt.Sprintf("₦%.2f", major)
	case "GHS":
		return fmt.Sprintf("GH₵%.2f", major)
	default:
		return fmt.Sprintf("%s %.2f", currency, major)
	}
}
