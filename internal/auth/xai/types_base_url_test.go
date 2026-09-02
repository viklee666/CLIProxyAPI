package xai

import "testing"

func TestSelectChatBaseURLUsesCLIChatProxyWhenUsingAPIIsFalse(t *testing.T) {
	tests := []struct {
		name     string
		baseURL  string
		usingAPI bool
		want     string
	}{
		{name: "empty without using_api rewrites to chat proxy", want: CLIChatProxyBaseURL},
		{name: "official default without using_api rewrites to chat proxy", baseURL: DefaultAPIBaseURL, want: CLIChatProxyBaseURL},
		{name: "official default trailing slash without using_api rewrites to chat proxy", baseURL: DefaultAPIBaseURL + "/", want: CLIChatProxyBaseURL},
		{name: "explicit chat proxy without using_api is preserved", baseURL: CLIChatProxyBaseURL, want: CLIChatProxyBaseURL},
		{name: "custom without using_api is honored", baseURL: "https://custom-gateway.example.com/v1", want: "https://custom-gateway.example.com/v1"},
		{name: "empty using_api keeps official api", usingAPI: true, want: DefaultAPIBaseURL},
		{name: "official default using_api keeps official api", baseURL: DefaultAPIBaseURL, usingAPI: true, want: DefaultAPIBaseURL},
		{name: "custom using_api is honored", baseURL: "https://custom-gateway.example.com/v1", usingAPI: true, want: "https://custom-gateway.example.com/v1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := SelectChatBaseURL(tt.baseURL, tt.usingAPI); got != tt.want {
				t.Fatalf("SelectChatBaseURL(%q, %v) = %q, want %q", tt.baseURL, tt.usingAPI, got, tt.want)
			}
		})
	}
}

func TestSelectMediaBaseURLKeepsOfficialAPIWhenUsingAPIIsFalse(t *testing.T) {
	tests := []struct {
		name     string
		baseURL  string
		usingAPI bool
		want     string
	}{
		{name: "empty without using_api stays on official api", want: DefaultAPIBaseURL},
		{name: "official default without using_api stays on official api", baseURL: DefaultAPIBaseURL, want: DefaultAPIBaseURL},
		{name: "official default trailing slash without using_api stays on official api", baseURL: DefaultAPIBaseURL + "/", want: DefaultAPIBaseURL},
		{name: "explicit chat proxy without using_api remaps to official api", baseURL: CLIChatProxyBaseURL, want: DefaultAPIBaseURL},
		{name: "explicit chat proxy trailing slash without using_api remaps to official api", baseURL: CLIChatProxyBaseURL + "/", want: DefaultAPIBaseURL},
		{name: "custom without using_api is honored", baseURL: "https://custom-gateway.example.com/v1", want: "https://custom-gateway.example.com/v1"},
		{name: "empty using_api keeps official api", usingAPI: true, want: DefaultAPIBaseURL},
		{name: "official default using_api keeps official api", baseURL: DefaultAPIBaseURL, usingAPI: true, want: DefaultAPIBaseURL},
		{name: "explicit chat proxy using_api remaps to official api", baseURL: CLIChatProxyBaseURL, usingAPI: true, want: DefaultAPIBaseURL},
		{name: "custom using_api is honored", baseURL: "https://custom-gateway.example.com/v1", usingAPI: true, want: "https://custom-gateway.example.com/v1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := SelectMediaBaseURL(tt.baseURL, tt.usingAPI); got != tt.want {
				t.Fatalf("SelectMediaBaseURL(%q, %v) = %q, want %q", tt.baseURL, tt.usingAPI, got, tt.want)
			}
		})
	}
}
