package index

import "testing"

func TestAnalyzer(t *testing.T) {
	tests := []struct {
		name string
		cfg  Config
		in   string
		want string
	}{
		{"bigram", Config{Mode: ModeBigram}, "東京都", "東京 京都"},
		{"bigram skips symbols", Config{Mode: ModeBigram}, "C++ と Go", "go"},
		{"bigram NFKC width", Config{Mode: ModeBigram}, "ＡＢＣとｶﾞｷﾞ", "ab bc cと とガ ガギ"},
		{"bigram NFKC composition", Config{Mode: ModeBigram}, "がき", "がき"},
		{"ja default stop tags, stop words and base forms", Config{Mode: ModeJa}, "東京都の天気を書いた", "東京 都 天気 書く"},
		{"ja default stop words", Config{Mode: ModeJa}, "これはペンです", "ペン"},
		{"ja only stop words", Config{Mode: ModeJa}, "の", ""},
		{"ja custom stop words", Config{Mode: ModeJa, StopWords: []string{"天気"}}, "東京都の天気", "東京 都"},
		{"ja custom stop words are normalized", Config{Mode: ModeJa, StopWords: []string{"ﾍﾟﾝ"}}, "ペンと紙", "紙"},
		{"ja no stop words or tags", Config{Mode: ModeJa, StopWords: []string{}, StopTags: []string{}}, "東京の天気", "東京 の 天気"},
		{"ja custom stop tags", Config{Mode: ModeJa, StopTags: []string{"名詞-固有名詞-地域-一般"}}, "東京の天気", "天気"},
		// Stop tags match exactly, as in Lucene: missing levels mean "*", not any.
		{"ja stop tags match exactly", Config{Mode: ModeJa, StopTags: []string{"名詞-固有名詞"}}, "東京の天気", "東京 天気"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			an, err := newAnalyzer(tt.cfg)
			if err != nil {
				t.Fatal(err)
			}
			if got := an.analyze(tt.in); got != tt.want {
				t.Errorf("analyze(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestAnalyzerFingerprint(t *testing.T) {
	fp := func(cfg Config) string {
		an, err := newAnalyzer(cfg)
		if err != nil {
			t.Fatal(err)
		}
		return an.fingerprint
	}
	def := fp(Config{Mode: ModeJa})
	if def != fp(Config{Mode: ModeJa}) {
		t.Error("fingerprint is not stable")
	}
	for _, cfg := range []Config{
		{Mode: ModeBigram},
		{Mode: ModeJa, StopWords: []string{"天気"}},
		{Mode: ModeJa, StopTags: []string{"助詞"}},
		{Mode: ModeJa, StopWords: []string{}},
	} {
		if fp(cfg) == def {
			t.Errorf("fingerprint of %+v equals the default", cfg)
		}
	}
}
