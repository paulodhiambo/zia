package ledger

const (
	AccountPSPClearingMpesa   = "psp_clearing:mpesa"
	AccountPSPClearingKCB     = "psp_clearing:kcb"
	AccountPSPClearingPaystack = "psp_clearing:paystack"

	AccountMerchantAvailable = "merchant:%s:available"
	AccountMerchantInTransit = "merchant:%s:in_transit"
	AccountMerchantSettled   = "merchant:%s:settled"

	AccountPlatformFees     = "platform:fees"
	AccountPlatformOperating = "platform:operating"
)

func MerchantAvailable(merchantID string) string {
	return formatAccount(AccountMerchantAvailable, merchantID)
}

func MerchantInTransit(merchantID string) string {
	return formatAccount(AccountMerchantInTransit, merchantID)
}

func MerchantSettled(merchantID string) string {
	return formatAccount(AccountMerchantSettled, merchantID)
}

func formatAccount(tmpl, id string) string {
	buf := make([]byte, 0, len(tmpl)+len(id)-2)
	for i := 0; i < len(tmpl); i++ {
		if tmpl[i] == '%' && i+1 < len(tmpl) && tmpl[i+1] == 's' {
			buf = append(buf, id...)
			i++
		} else {
			buf = append(buf, tmpl[i])
		}
	}
	return string(buf)
}
