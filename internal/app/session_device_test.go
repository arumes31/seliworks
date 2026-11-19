package app

import "testing"

func TestDeviceClass(t *testing.T) {
	const (
		iphoneSafari16 = "Mozilla/5.0 (iPhone; CPU iPhone OS 16_6 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/16.6 Mobile/15E148 Safari/604.1"
		iphoneSafari18 = "Mozilla/5.0 (iPhone; CPU iPhone OS 18_1 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/18.1 Mobile/15E148 Safari/604.1"
		// Home-screen apps on iOS omit the Version and Safari tokens.
		iphonePWA    = "Mozilla/5.0 (iPhone; CPU iPhone OS 18_1 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Mobile/15E148"
		iphoneChrome = "Mozilla/5.0 (iPhone; CPU iPhone OS 18_1 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) CriOS/131.0.6778.73 Mobile/15E148 Safari/604.1"
		macDesktop   = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/18.1 Safari/605.1.15"
		android      = "Mozilla/5.0 (Linux; Android 14; Pixel 8) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Mobile Safari/537.36"
		windowsEdge  = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36 Edg/131.0.0.0"
		windowsFF    = "Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:133.0) Gecko/20100101 Firefox/133.0"
		androidDesk  = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36"
		chromebook   = "Mozilla/5.0 (X11; CrOS x86_64 14541.0.0) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36"
	)
	for _, tc := range []struct{ ua, want string }{
		{iphoneSafari16, "apple/safari"},
		{iphonePWA, "apple/safari"},
		{iphoneChrome, "apple/chrome"},
		{macDesktop, "apple/safari"},
		{android, "linux/chrome"},
		{windowsEdge, "windows/edge"},
		{windowsFF, "windows/firefox"},
		{chromebook, "chromeos/chrome"},
		{"", "other/other"},
	} {
		if got := deviceClass(tc.ua); got != tc.want {
			t.Errorf("deviceClass(%q) = %q, want %q", tc.ua, got, tc.want)
		}
	}
	// OS updates and switching between Safari and the home-screen app keep the class.
	if deviceClass(iphoneSafari16) != deviceClass(iphoneSafari18) || deviceClass(iphoneSafari18) != deviceClass(iphonePWA) {
		t.Error("iPhone OS update or home-screen launch changed the device class")
	}
	// "Request Desktop Website" on iPhone and Android keeps the class.
	if deviceClass(iphoneSafari18) != deviceClass(macDesktop) || deviceClass(android) != deviceClass(androidDesk) {
		t.Error("desktop-mode toggle changed the device class")
	}
	if len(deviceClass(windowsEdge)) > 64 {
		t.Error("device class exceeds the database column limit")
	}
}
