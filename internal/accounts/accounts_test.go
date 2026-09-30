package accounts

import "testing"

func TestInferType(t *testing.T) {
	cases := []struct {
		name string
		bal  int64
		want string
	}{
		{"360 Checking (1111)", 100, Checking},
		{"360 Performance Savings", 100, Savings},
		{"Quicksilver Card (3333)", -100, CreditCard},
		{"Fidelity Roth IRA", 100, Investment},
		{"Miracle Account", 100, Checking},
		{"Mystery", -100, CreditCard},
		{"Home Mortgage", -100, Mortgage},
		{"Auto Loan", -100, Loan},
		{"Spirit Airlines", 100, Checking}, // "ira" must be a whole word
	}
	for _, c := range cases {
		if got := InferType(c.name, c.bal); got != c.want {
			t.Errorf("InferType(%q) = %s, want %s", c.name, got, c.want)
		}
	}
}

func TestMask(t *testing.T) {
	cases := map[string]string{
		"Card (1234)":      "1234",
		"Card (...1234)":   "1234",
		"Checking ...5678": "5678",
		"Visa x9012":       "9012",
		"Account #3456":    "3456",
		"Savor Card":       "",
		"Plan 2024":        "2024",
		"Rewards12345":     "",
	}
	for in, want := range cases {
		if got := Mask(in); got != want {
			t.Errorf("Mask(%q) = %q, want %q", in, got, want)
		}
	}
}
