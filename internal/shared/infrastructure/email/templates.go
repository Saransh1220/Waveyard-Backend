package email

import (
	"bytes"
	"embed"
	"fmt"
	"html/template"
	"net/url"
	"strings"
)

//go:embed templates/*.html
var templateFS embed.FS

type ReceiptData struct {
	BuyerName            string
	BuyerEmail           string
	SpecTitle            string
	LicenseType          string
	AmountDisplay        string
	OrderID              string
	PaymentID            string
	LicenseID            string
	SupportEmail         string
	OriginalPriceDisplay string
	SavingsDisplay       string
	TaxDisplay           string
}

type emailCTA struct {
	Label string
	URL   string
}

type emailMetaRow struct {
	Label string
	Value string
}

type emailTemplateData struct {
	BrandName    string
	Preheader    string
	Eyebrow      string
	Title        string
	Greeting     string
	Intro        []string
	CodeLabel    string
	Code         string
	NoticeTitle  string
	NoticeBody   string
	PrimaryCTA   *emailCTA
	SecondaryCTA *emailCTA
	MetaRows     []emailMetaRow
	FooterNote   string
	SupportEmail string
}

func BuildVerificationEmail(toEmail, displayName, code, appBaseURL string) Message {
	name := displayNameOrFallback(displayName)
	link := buildLink(appBaseURL, "/verify-email", map[string]string{
		"email": toEmail,
		"code":  code,
	})
	subject := "Verify your Blueprint account"
	data := emailTemplateData{
		BrandName: "BLUEPRINT",
		Preheader: "Your Blueprint verification code is inside. Finish setting up your account in seconds.",
		Eyebrow:   "Account verification",
		Title:     "Confirm your email and step into the studio.",
		Greeting:  fmt.Sprintf("Hi %s,", name),
		Intro: []string{
			"Use the verification code below to activate your Blueprint account.",
			"If you prefer, you can also open the verification page directly from the button below.",
		},
		CodeLabel:   "Verification code",
		Code:        code,
		NoticeTitle: "Didn't sign up?",
		NoticeBody:  "If you did not create a Blueprint account, you can safely ignore this email.",
		PrimaryCTA: &emailCTA{
			Label: "Verify Email",
			URL:   link,
		},
		SecondaryCTA: &emailCTA{
			Label: "Open verification page",
			URL:   link,
		},
		FooterNote: "This code expires in 15 minutes.",
	}

	return Message{
		To:      []string{toEmail},
		Subject: subject,
		Text:    buildVerificationText(name, code, link),
		HTML:    mustRenderTemplate("verify-email.html", data),
	}
}

func BuildPasswordResetEmail(toEmail, displayName, code, appBaseURL string) Message {
	name := displayNameOrFallback(displayName)
	link := buildLink(appBaseURL, "/reset-password", map[string]string{
		"email": toEmail,
		"code":  code,
	})
	subject := "Reset your Blueprint password"
	data := emailTemplateData{
		BrandName: "BLUEPRINT",
		Preheader: "Use this code to reset your Blueprint password and secure your account.",
		Eyebrow:   "Security request",
		Title:     "Reset your password without missing a beat.",
		Greeting:  fmt.Sprintf("Hi %s,", name),
		Intro: []string{
			"We received a request to reset your Blueprint password.",
			"Enter the code below to continue, or use the secure reset button if you're already on your device.",
		},
		CodeLabel:   "Reset code",
		Code:        code,
		NoticeTitle: "Wasn't you?",
		NoticeBody:  "If you did not request a password reset, ignore this email. Your password stays unchanged until you complete the reset flow.",
		PrimaryCTA: &emailCTA{
			Label: "Reset Password",
			URL:   link,
		},
		SecondaryCTA: &emailCTA{
			Label: "Open reset page",
			URL:   link,
		},
		FooterNote: "This code expires in 15 minutes. For your security, existing sessions will be revoked after a successful reset.",
	}

	return Message{
		To:      []string{toEmail},
		Subject: subject,
		Text:    buildPasswordResetText(name, code, link),
		HTML:    mustRenderTemplate("reset-password.html", data),
	}
}

func BuildPaymentReceiptEmail(data ReceiptData, appBaseURL string) Message {
	name := displayNameOrFallback(data.BuyerName)
	link := buildLink(appBaseURL, "/licenses", nil)
	subject := "Your Blueprint purchase receipt"
	viewData := emailTemplateData{
		BrandName: "BLUEPRINT",
		Preheader: "Your purchase is confirmed. Access your license details and downloads from Blueprint.",
		Eyebrow:   "Purchase confirmed",
		Title:     "Your new license is ready.",
		Greeting:  fmt.Sprintf("Hi %s,", name),
		Intro: []string{
			"Your order has been confirmed and your license is now available in Blueprint.",
			"Keep this receipt for your records. If you ever need help, the order and payment references below will help support find your purchase quickly.",
		},
		NoticeTitle: "Need files or license details?",
		NoticeBody:  "Open your licenses to review the purchase, download your files, and revisit access later from your account dashboard.",
		PrimaryCTA: &emailCTA{
			Label: "Open My Licenses",
			URL:   link,
		},
		SecondaryCTA: &emailCTA{
			Label: "Open purchase dashboard",
			URL:   link,
		},
		MetaRows:     paymentReceiptRows(data),
		FooterNote:   "This email is your purchase receipt from Blueprint.",
		SupportEmail: strings.TrimSpace(data.SupportEmail),
	}

	return Message{
		To:      []string{data.BuyerEmail},
		Subject: subject,
		Text:    buildPaymentReceiptText(name, data, link),
		HTML:    mustRenderTemplate("payment-receipt.html", viewData),
	}
}

func paymentReceiptRows(data ReceiptData) []emailMetaRow {
	rows := []emailMetaRow{
		{Label: "Title", Value: data.SpecTitle},
		{Label: "License", Value: data.LicenseType},
	}
	if data.SavingsDisplay != "" {
		rows = append(rows,
			emailMetaRow{Label: "Original price", Value: data.OriginalPriceDisplay},
			emailMetaRow{Label: "Coupon savings", Value: data.SavingsDisplay},
		)
	}
	if data.TaxDisplay != "" {
		rows = append(rows, emailMetaRow{Label: "Tax", Value: data.TaxDisplay})
	}
	return append(rows,
		emailMetaRow{Label: "Amount paid", Value: data.AmountDisplay},
		emailMetaRow{Label: "Order ID", Value: data.OrderID},
		emailMetaRow{Label: "Payment ID", Value: data.PaymentID},
		emailMetaRow{Label: "License ID", Value: data.LicenseID},
	)
}

func displayNameOrFallback(displayName string) string {
	name := strings.TrimSpace(displayName)
	if name == "" {
		return "there"
	}
	return name
}

func mustRenderTemplate(name string, data emailTemplateData) string {
	html, err := renderTemplate(name, data)
	if err != nil {
		panic(err)
	}
	return html
}

func renderTemplate(name string, data emailTemplateData) (string, error) {
	tpl, err := template.ParseFS(templateFS, "templates/layout.html", "templates/"+name)
	if err != nil {
		return "", fmt.Errorf("parse email template %s: %w", name, err)
	}

	var buf bytes.Buffer
	if err := tpl.ExecuteTemplate(&buf, "layout", data); err != nil {
		return "", fmt.Errorf("render email template %s: %w", name, err)
	}
	return buf.String(), nil
}

func buildVerificationText(name, code, link string) string {
	return fmt.Sprintf(
		"Hi %s,\n\nUse this verification code to activate your Blueprint account:\n%s\n\nVerify your email here:\n%s\n\nIf you did not create this account, you can ignore this email.\n\nThis code expires in 15 minutes.",
		name,
		code,
		link,
	)
}

func buildPasswordResetText(name, code, link string) string {
	return fmt.Sprintf(
		"Hi %s,\n\nWe received a request to reset your Blueprint password.\n\nYour reset code:\n%s\n\nReset your password here:\n%s\n\nIf you did not request this, you can ignore this email. Your password will remain unchanged until the reset is completed.\n\nThis code expires in 15 minutes.",
		name,
		code,
		link,
	)
}

func buildPaymentReceiptText(name string, data ReceiptData, link string) string {
	lines := []string{
		fmt.Sprintf("Hi %s,", name),
		"",
		"Your purchase is confirmed and your license is ready.",
		"",
	}
	for _, row := range paymentReceiptRows(data) {
		lines = append(lines, fmt.Sprintf("%s: %s", row.Label, row.Value))
	}
	lines = append(lines, "", "Open your licenses here:", link)
	if support := strings.TrimSpace(data.SupportEmail); support != "" {
		lines = append(lines, "", fmt.Sprintf("Need help? Contact %s", support))
	}
	lines = append(lines, "", "Keep this email for your records.")
	return strings.Join(lines, "\n")
}

func buildLink(baseURL, path string, params map[string]string) string {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		return path
	}
	u, err := url.Parse(baseURL + path)
	if err != nil {
		return baseURL + path
	}
	q := u.Query()
	for k, v := range params {
		q.Set(k, v)
	}
	u.RawQuery = q.Encode()
	return u.String()
}
