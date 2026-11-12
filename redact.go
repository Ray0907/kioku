package main

import (
	"regexp"
	"strings"
)

var redactPatterns = []struct {
	prefixes []string
	pattern  *regexp.Regexp
}{
	{[]string{"-----BEGIN "}, regexp.MustCompile(`-----BEGIN ((?:[A-Z0-9]+ )*PRIVATE KEY)-----`)},
	{[]string{"Authorization:"}, regexp.MustCompile(`\b((?i:Authorization:[ \t]*Bearer[ \t]+))[A-Za-z0-9._~+/-]+=*`)},
	{[]string{"sk-"}, regexp.MustCompile(`\bsk-[A-Za-z0-9_-]{20,}`)},
	{[]string{"ghp_", "gho_", "ghu_", "ghs_", "ghr_", "github_pat_"}, regexp.MustCompile(`\b(?:gh[pousr]_|github_pat_)[A-Za-z0-9_]{20,}`)},
	{[]string{"AKIA", "ASIA"}, regexp.MustCompile(`\b(?:AKIA|ASIA)[A-Z0-9]{16}\b`)},
	{[]string{"xoxb-", "xoxp-", "xoxa-", "xoxr-", "xoxs-"}, regexp.MustCompile(`\bxox[bpars]-[A-Za-z0-9_-]{20,}`)},
	{[]string{"AIza"}, regexp.MustCompile(`\bAIza[A-Za-z0-9_-]{35}`)},
	{[]string{"sk_live_", "rk_live_"}, regexp.MustCompile(`\b(?:sk_live_|rk_live_)[A-Za-z0-9_-]+`)},
	{[]string{"eyJ"}, regexp.MustCompile(`\beyJ[A-Za-z0-9_-]*\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+`)},
}

func redact(text string) string {
	for kind, rule := range redactPatterns {
		found := false
		for _, prefix := range rule.prefixes {
			if strings.Contains(text, prefix) {
				found = true
				break
			}
		}
		if !found && kind == 1 {
			// Preserve case-insensitive headers without copying a credential-free input.
			for start := 0; start < len(text); {
				at := strings.IndexByte(text[start:], ':')
				if at < 0 {
					break
				}
				at += start
				if at >= len("Authorization") && strings.EqualFold(text[at-len("Authorization"):at], "Authorization") {
					found = true
					break
				}
				start = at + 1
			}
		}
		if !found {
			continue
		}
		var b strings.Builder
		start := 0
		for {
			match := rule.pattern.FindStringSubmatchIndex(text[start:])
			if match == nil {
				break
			}
			from, end := start+match[0], start+match[1]
			b.WriteString(text[start:from])
			switch kind {
			case 0:
				// RE2 has no backreferences; consume only the matching PEM end label, or EOF.
				endMarker := "-----END " + text[start+match[2]:start+match[3]] + "-----"
				if at := strings.Index(text[end:], endMarker); at >= 0 {
					end += at + len(endMarker)
				} else {
					end = len(text)
				}
			case 1:
				b.WriteString(text[start+match[2] : start+match[3]])
			}
			b.WriteString("****")
			start = end
		}
		if start > 0 {
			b.WriteString(text[start:])
			text = b.String()
		}
	}
	return text
}
