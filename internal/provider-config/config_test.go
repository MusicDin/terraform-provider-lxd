package config

import "testing"

func TestDetermineLXDAddress(t *testing.T) {
	tests := []struct {
		Name      string
		Protocol  string
		Address   string
		Scheme    string
		Port      string
		Expect    string
		ExpectErr bool
	}{
		{
			Name:    "Empty address",
			Address: "",
			Expect:  "unix://",
		},
		{
			Name:    "Fully qualified https address",
			Address: "https://localhost:8443",
			Expect:  "https://localhost:8443",
		},
		{
			Name:    "Fully qualified unix address",
			Address: "https://localhost:8443",
			Expect:  "https://localhost:8443",
		},
		{
			Name:    "Construct https address",
			Address: "localhost",
			Scheme:  "https",
			Port:    "1234",
			Expect:  "https://localhost:1234",
		},
		{
			Name:    "Construct unix address",
			Address: "/path/to/socket",
			Scheme:  "unix",
			Expect:  "unix:///path/to/socket",
		},
		{
			Name:     "Default https port",
			Protocol: "lxd",
			Scheme:   "https",
			Address:  "localhost",
			Expect:   "https://localhost:8443",
		},
		{
			Name:     "Default simplestreams https port",
			Protocol: "simplestreams",
			Scheme:   "https",
			Address:  "localhost",
			Expect:   "https://localhost:443",
		},
		{
			Name:    "Unix path as address",
			Address: "/abs/path",
			Expect:  "unix:///abs/path",
		},
		{
			Name:      "Duplicate port configuration",
			Address:   "https://localhost:8443",
			Port:      "8443",
			ExpectErr: true,
		},
		{
			Name:      "Duplicate scheme configuration",
			Address:   "https://localhost:8443",
			Scheme:    "https",
			ExpectErr: true,
		},
		{
			Name:      "Unsupported scheme",
			Address:   "http://localhost:8443",
			ExpectErr: true,
		},
	}

	for _, test := range tests {
		t.Run(test.Name, func(t *testing.T) {
			addr, err := DetermineLXDAddress(test.Protocol, test.Scheme, test.Address, test.Port)
			if err != nil && !test.ExpectErr {
				t.Fatalf("Unexpected error: %v", err)
			}

			if err == nil && test.ExpectErr {
				t.Fatalf("Expected an error, but got none")
			}

			if addr != test.Expect {
				t.Fatalf("Expected address %q, got %q", test.Expect, addr)
			}
		})
	}
}
