package main

import "testing"

func TestClassifyEndpointScopeAllowsLoopbackAndLocalNetworkIPs(t *testing.T) {
	tests := []struct {
		name    string
		baseURL string
		want    endpointScope
	}{
		{name: "localhost", baseURL: "http://localhost:11434/v1", want: endpointScopeLoopback},
		{name: "ipv4 loopback", baseURL: "http://127.0.0.1:11434/v1", want: endpointScopeLoopback},
		{name: "ipv6 loopback", baseURL: "http://[::1]:11434/v1", want: endpointScopeLoopback},
		{name: "rfc1918 10/8", baseURL: "http://10.20.30.40:11434/v1", want: endpointScopeLocalNetwork},
		{name: "rfc1918 172.16/12", baseURL: "http://172.20.1.2:11434/v1", want: endpointScopeLocalNetwork},
		{name: "rfc1918 192.168/16", baseURL: "http://192.168.1.50:11434/v1", want: endpointScopeLocalNetwork},
		{name: "ipv4 link local", baseURL: "http://169.254.1.50:11434/v1", want: endpointScopeLocalNetwork},
		{name: "shared vpn space", baseURL: "http://100.64.12.34:11434/v1", want: endpointScopeLocalNetwork},
		{name: "ipv6 unique local", baseURL: "http://[fd00::1]:11434/v1", want: endpointScopeLocalNetwork},
		{name: "ipv6 link local", baseURL: "http://[fe80::1]:11434/v1", want: endpointScopeLocalNetwork},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, detail := classifyEndpointScope(tt.baseURL)
			if got != tt.want {
				t.Fatalf("classifyEndpointScope(%q) scope = %q, want %q (detail: %s)", tt.baseURL, got, tt.want, detail)
			}
			if detail != "" {
				t.Fatalf("classifyEndpointScope(%q) detail = %q, want empty", tt.baseURL, detail)
			}
		})
	}
}

func TestClassifyEndpointScopeFlagsRemoteAndInvalidEndpoints(t *testing.T) {
	tests := []struct {
		name    string
		baseURL string
		want    endpointScope
	}{
		{name: "remote ipv4", baseURL: "https://203.0.113.10/v1", want: endpointScopeRemote},
		{name: "remote hostname", baseURL: "https://llm.example.com/v1", want: endpointScopeRemote},
		{name: "unsupported scheme", baseURL: "ftp://127.0.0.1/v1", want: endpointScopeInvalid},
		{name: "missing host", baseURL: "http:///v1", want: endpointScopeInvalid},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, detail := classifyEndpointScope(tt.baseURL)
			if got != tt.want {
				t.Fatalf("classifyEndpointScope(%q) scope = %q, want %q", tt.baseURL, got, tt.want)
			}
			if detail == "" {
				t.Fatalf("classifyEndpointScope(%q) detail is empty, want explanation", tt.baseURL)
			}
		})
	}
}
