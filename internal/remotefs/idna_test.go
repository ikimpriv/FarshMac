package remotefs

import "testing"

func TestParseConvertsIDNHost(t *testing.T) {
	for in, want := range map[string]string{
		"sftp://пример.рф/":     "xn--e1afmkfd.xn--p1ai",
		"ftp://Bücher.example/": "xn--bcher-kva.example",
		"sftp://例え.テスト:2222/x":  "xn--r8jz45g.xn--zckzah",
		"sftp://plain.example/": "plain.example",
	} {
		c, err := Parse(in, "u")
		if err != nil || c.Host != want {
			t.Fatalf("%s -> %q, %v", in, c.Host, err)
		}
		if got := hostToUnicode(c.Host); in != "sftp://plain.example/" && got == c.Host {
			t.Fatalf("label stays ASCII for %s", in)
		}
	}
	c, _ := Parse("sftp://пример.рф:2200/", "user")
	if c.Label() != "sftp://user@пример.рф:2200" {
		t.Fatal("label", c.Label())
	}
}
