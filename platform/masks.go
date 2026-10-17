package platform

import (
	"cmp"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
)

// Removed is what a mask writes in place of the text it hides.
const Removed = "<removed>"

// CompileMasks compiles a platform's masks: regular expressions in Go's
// syntax, each holding at least one capturing group. It is the compiler of
// the list for the configuration's check (config_platform_mask_invalid),
// the audit, and the collection writer; the plan's check
// (execution_plan_invalid) makes the same two checks itself, since that
// package imports only inventory. On a pattern that does not compile or
// holds no group it returns that entry's index and the reason, worded to
// follow the pattern ("does not compile: …", "holds no capturing group");
// the caller names the code.
func CompileMasks(list []string) ([]*regexp.Regexp, int, error) {
	if len(list) == 0 {
		return nil, -1, nil
	}
	res := make([]*regexp.Regexp, 0, len(list))
	for i, p := range list {
		re, err := regexp.Compile(p)
		if err != nil {
			return nil, i, fmt.Errorf("does not compile: %w", err)
		}
		// A pattern with no group would match and hide nothing.
		if re.NumSubexp() == 0 {
			return nil, i, errors.New("holds no capturing group")
		}
		res = append(res, re)
	}
	return res, -1, nil
}

// Mask returns s with every capturing group of every match of every
// pattern replaced by Removed. Text that more than one group covers is
// replaced once, and a group that matched no text is left alone.
func Mask(res []*regexp.Regexp, s string) string {
	spans := maskSpans(res, s)
	if len(spans) == 0 {
		return s
	}
	var b strings.Builder
	at := 0
	for _, sp := range spans {
		b.WriteString(s[at:sp[0]])
		b.WriteString(Removed)
		at = sp[1]
	}
	b.WriteString(s[at:])
	return b.String()
}

// maskSpans is the text Mask replaces: the byte ranges of every non-empty
// group of every match, in order, those that overlap merged.
func maskSpans(res []*regexp.Regexp, s string) [][2]int {
	var spans [][2]int
	for _, re := range res {
		for _, m := range re.FindAllStringSubmatchIndex(s, -1) {
			for g := 2; g+1 < len(m); g += 2 {
				if m[g] >= 0 && m[g+1] > m[g] {
					spans = append(spans, [2]int{m[g], m[g+1]})
				}
			}
		}
	}
	if len(spans) < 2 {
		return spans
	}
	slices.SortFunc(spans, func(a, b [2]int) int { return cmp.Compare(a[0], b[0]) })
	merged := spans[:1]
	for _, sp := range spans[1:] {
		if last := &merged[len(merged)-1]; sp[0] < last[1] {
			last[1] = max(last[1], sp[1])
			continue
		}
		merged = append(merged, sp)
	}
	return merged
}

// The built-in masks (docs/EXAMPLES.md, chapter 63, issue 8): oxidized's
// rules their start, each written for a statement as sent (a command's no
// form, the type digit optional, the common abbreviations) and matching the
// line the device prints too, so that one list serves the audit and the
// collection. Go's matcher takes the first alternative that matches at a
// position, so an alternative holding no group, listed first, keeps a word
// that holds no secret whole.

var iosxeMasks = []string{
	// SNMP: community, a host's v1/v2c community, a v3 user's auth and priv
	`(?i)^\s*(?:no\s+)?snmp-s\S*\s+comm\S*\s+(\S+)`,
	`(?i)^\s*(?:no\s+)?snmp-s\S*\s+host\s+\S+(?:\s+vrf\s+\S+)?(?:\s+(?:informs?|traps?))?(?:\s+version\s+(?:1|2c|3\s+\S+))?\s+(\S+)`,
	`(?i)^\s*(?:no\s+)?snmp-s\S*\s+user\s.*?\bauth\s+(?:md5|sha\S*(?:\s+\d{3})?)\s+(\S+)`,
	`(?i)^\s*(?:no\s+)?snmp-s\S*\s+user\s.*?\bpriv\s+(?:des\S*|3des|aes(?:\s+\d{3})?)\s+(\S+)`,
	// password or secret anywhere: username, enable (with level), line, BGP
	// neighbor, PPP, IS-IS, dot1x; abbreviations pass… and sec…; encryption
	// holds no secret
	`(?i)\b(?:pass(?:w(?:o(?:r(?:d)?)?)?)?|sec(?:r(?:e(?:t)?)?)?)\s+(?:encryption\s|(?:level\s+\d+\s+)?(?:\d\s+)?(\S+))`,
	`(?i)\bpassword-encrypt\s+(\S+)`,
	// keys: tacacs/radius server's bare key, legacy -server key, server-key and
	// pac key, ISAKMP, IKEv2 pre-shared and pre-share, OSPF and NTP, key chains
	// and HSRP key-string
	`(?i)^\s*(?:no\s+)?key\s+(?:\d\s+)?(\S+)\s*$`,
	`(?i)^\s*(?:no\s+)?(?:tacacs|radius)-server\s.*?\bkey\s+(?:\d\s+)?(\S+)`,
	`(?i)\b(?:server-key|pac\s+key)\s+(?:\d\s+)?(\S+)`,
	`(?i)^\s*(?:no\s+)?crypto\s+isakmp\s+key\s+(?:\d\s+)?(\S+)`,
	`(?i)\bpre-shared-key\s+(?:(?:local|remote)\s+)?(?:\d\s+)?(\S+)`,
	`(?i)\bpre-share\s+key\s+(?:\d\s+)?(\S+)`,
	`(?i)\bauthentication-key\s+(?:\d+\s+)?(?:(?:md5|sha\S*|cmac\S*|hmac\S*)\s+)?(?:\d\s+)?(\S+)`,
	`(?i)\bmessage-digest-key\s+\d+\s+md5\s+(?:\d\s+)?(\S+)`,
	`(?i)\bkey-string\s+(?:\d\s+)?(\S+)`,
	// HSRP and VRRP plain text, wireless PSK, NHRP, a URL's password
	`(?i)^\s*(?:no\s+)?(?:standby|vrrp)\s+\d+\s+authentication\s+(?:text\s+)?(\S+)\s*$`,
	`(?i)\b(?:wpa-psk|set-key)\s+(?:ascii|hex)\s+(?:\d\s+)?(\S+)`,
	`(?i)\bnhrp\s+authentication\s+(\S+)`,
	`(?i)\b(?:ftp|tftp|scp|sftp|https?)://[^/\s:@]+:([^@\s]+)@`,
}

var iosxrMasks = []string{
	// SNMP: community, a host's community, a v3 user's auth and priv
	`(?i)^\s*(?:no\s+)?snmp-s\S*\s+comm\S*\s+(?:(?:encrypted|clear)\s+)?(\S+)`,
	`(?i)^\s*(?:no\s+)?snmp-s\S*\s+host\s+\S+(?:\s+vrf\s+\S+)?(?:\s+(?:informs?|traps?))?(?:\s+version\s+(?:1|2c|3\s+\S+))?(?:\s+(?:encrypted|clear))?\s+(\S+)`,
	`(?i)^\s*(?:no\s+)?snmp-s\S*\s+user\s.*?\bauth\s+(?:md5|sha\S*(?:\s+\d{3})?)\s+(?:(?:encrypted|clear)\s+)?(\S+)`,
	`(?i)^\s*(?:no\s+)?snmp-s\S*\s+user\s.*?\bpriv\s+(?:des\S*|3des|aes(?:\s+\d{3})?)\s+(?:(?:encrypted|clear)\s+)?(\S+)`,
	// password or secret anywhere: username, BGP and LDP neighbor, IS-IS
	// lsp- and hello-password; abbreviations pass… and sec…
	`(?i)\b(?:pass(?:w(?:o(?:r(?:d)?)?)?)?|sec(?:r(?:e(?:t)?)?)?)\s+(?:(?:hmac-md5|text)\s+)?(?:(?:encrypted|clear|\d{1,2})\s+)?(\S+)`,
	// keys: a tacacs/radius host's bare key, -server key, server-key, IKEv2,
	// OSPF and NTP, key chains
	`(?i)^\s*(?:no\s+)?key\s+(?:(?:encrypted|clear|\d)\s+)?(\S+)\s*$`,
	`(?i)^\s*(?:no\s+)?(?:tacacs|radius)-server\s.*?\bkey\s+(?:(?:encrypted|clear|\d)\s+)?(\S+)`,
	`(?i)\bserver-key\s+(?:(?:encrypted|clear|\d)\s+)?(\S+)`,
	`(?i)\bpre-shared-key\s+(?:(?:local|remote)\s+)?(?:(?:clear|password|\d)\s+)?(\S+)`,
	`(?i)\bauthentication-key\s+(?:\d+\s+)?(?:(?:md5|sha\S*|hmac\S*)\s+)?(?:(?:encrypted|clear|\d)\s+)?(\S+)`,
	`(?i)\bmessage-digest-key\s+\d+\s+md5\s+(?:(?:encrypted|clear|\d)\s+)?(\S+)`,
	`(?i)\bkey-string\s+(?:(?:clear|password|\d)\s+)?(\S+)`,
	// HSRP plain text (8 characters at most), VRRP text, a URL's password
	`(?i)^\s*(?:no\s+)?authentication\s+(\S{1,8})\s*$`,
	`(?i)\btext-authentication\s+(\S+)`,
	`(?i)\b(?:ftp|tftp|scp|sftp|https?)://[^/\s:@]+:([^@\s]+)@`,
}

var nxosMasks = []string{
	// SNMP: community, a host's community after traps or informs, the MIB
	// community map, a v3 user's auth and priv (printed as localized keys too)
	`(?i)^\s*(?:no\s+)?snmp-s\S*\s+comm\S*\s+("[^"]*"|\S+)`,
	`(?i)^\s*(?:no\s+)?snmp-s\S*\s+host\s+\S+\s+(?:informs|traps)(?:\s+version\s+(?:1|2c|3\s+\S+))?\s+("[^"]*"|\S+)`,
	`(?i)^\s*(?:no\s+)?snmp-s\S*\s+mib\s+community-map\s+("[^"]*"|\S+)`,
	`(?i)^\s*(?:no\s+)?snmp-s\S*\s+user\s.*?\bauth\s+(?:md5|sha\S*)\s+(\S+)`,
	`(?i)^\s*(?:no\s+)?snmp-s\S*\s+user\s.*?\bpriv\s+(?:aes-128\s+)?(\S+)`,
	// password or secret anywhere: username, enable, BGP neighbor, LDAP;
	// strength-check, secure-mode, and encryption hold no secret
	`(?i)\b(?:pass(?:w(?:o(?:r(?:d)?)?)?)?|sec(?:r(?:e(?:t)?)?)?)\s+(?:strength-check|secure-mode|encryption\s|(?:\d\s+)?("[^"]*"|\S+))`,
	// keys: tacacs/radius, OSPF and NTP, key chains and HSRP key-string
	`(?i)^\s*(?:no\s+)?(?:tacacs|radius)-server\s.*?\bkey\s+(?:\d\s+)?("[^"]*"|\S+)`,
	`(?i)\bauthentication-key\s+(?:\d+\s+)?(?:(?:md5|sha\S*)\s+)?(?:\d\s+)?("[^"]*"|\S+)`,
	`(?i)\bmessage-digest-key\s+\d+\s+md5\s+(?:\d\s+)?("[^"]*"|\S+)`,
	`(?i)\bkey-string\s+(?:\d\s+)?("[^"]*"|\S+)`,
	// HSRP and VRRP text, PIM hello, a URL's password
	`(?i)^\s*(?:no\s+)?authentication\s+text\s+("[^"]*"|\S+)`,
	`(?i)\bhello-authentication\s+ah-md5\s+(?:\d\s+)?("[^"]*"|\S+)`,
	`(?i)\b(?:ftp|tftp|scp|sftp|https?)://[^/\s:@]+:([^@\s]+)@`,
}

var eosMasks = []string{
	// SNMP: community, a host's community, a v3 user's auth and priv
	`(?i)^\s*(?:no\s+)?snmp-s\S*\s+comm\S*\s+(\S+)`,
	`(?i)^\s*(?:no\s+)?snmp-s\S*\s+host\s+\S+(?:\s+vrf\s+\S+)?(?:\s+(?:informs?|traps?))?(?:\s+version\s+(?:1|2c|3\s+\S+))?\s+(\S+)`,
	`(?i)^\s*(?:no\s+)?snmp-s\S*\s+user\s.*?\bauth\s+(?:md5|sha\d{0,3})\s+(\S+)`,
	`(?i)^\s*(?:no\s+)?snmp-s\S*\s+user\s.*?\bpriv\s+(?:des|3des|aes\d{0,3})\s+(\S+)`,
	// password or secret as a word: username, enable, aaa root, BGP neighbor,
	// a shared-secret profile's key; management security's minimum, policy,
	// and encryption-key hold no secret
	`(?i)(?:^|\s)(?:pass(?:w(?:o(?:r(?:d)?)?)?)?|sec(?:r(?:e(?:t)?)?)?)\s+(?:minimum\s|policy\s|encryption\S*\s|\d+\s+\d\S*\s+(\S+)\s+receive-lifetime|(?:(?:\d|sha512)\s+)?(\S+))`,
	// keys: tacacs/radius, OSPF and NTP, IS-IS and VRRP, key-string, MACsec CAK
	`(?i)^\s*(?:no\s+)?(?:tacacs|radius)-server\s.*?\bkey\s+(?:\d\s+)?(\S+)`,
	`(?i)\bauthentication-key\s+(?:\d+\s+)?(?:(?:md5|sha\S*)\s+)?(?:\d\s+)?(\S+)`,
	`(?i)\bmessage-digest-key\s+\d+\s+md5\s+(?:\d\s+)?(\S+)`,
	`(?i)\bauthentication\s+(?:key|text)\s+(?:\d\s+)?(\S+)`,
	`(?i)\bkey-string\s+(?:\d\s+)?(\S+)`,
	`(?i)^\s*key\s+[0-9a-f]+\s+\d\s+(\S+)`,
	// the licence key, a URL's password
	`(?i)\bunsupported-transceiver\s+\S+\s+(\S+)`,
	`(?i)\b(?:ftp|tftp|scp|sftp|https?):(?://)?[^/\s:@]+:([^@\s]+)@`,
}

var junosMasks = []string{
	// the printed form: Junos tags each secret line itself
	`(?i)\s("[^"]*"|[^\s;]+);\s*## SECRET-DATA`,
	// SNMP: the community in a statement, v3's community-name, a printed block
	`(?i)\bsnmp\s+community\s+("[^"]*"|\S+)`,
	`(?i)^\s*community\s+("[^"]*"|\S+)\s*\{`,
	`(?i)\bcommunity-name\s+("[^"]*"|[^\s;]+)`,
	// authentication-key (BGP, IS-IS, VRRP, v3), NTP's value first
	`(?i)\bauthentication-key\s+(?:\d+\s+type\s+\S+\s+value\s+("[^"]*"|[^\s;]+)|("[^"]*"|[^\s;]+))`,
	// passwords and secrets: login, v3 users, OSPF simple, RADIUS, TACACS+,
	// key chains; login password rules and a printed block hold no secret
	`(?i)\b(?:encrypted-password|plain-text-password-value|authentication-password|privacy-password|simple-password|privacy-key|secret)\s+("[^"]*"|[^\s;]+)`,
	`(?i)\bpassword\s+(?:(?:minimum|maximum)-\S+\s|format\s|change-type\s|[{;]|("[^"]*"|[^\s;]+))`,
	// OSPF MD5, IKE, a URL's password
	`(?i)\bmd5\s+\d+\s+key\s+("[^"]*"|[^\s;]+)`,
	`(?i)\bpre-shared-key\s+(?:ascii-text|hexadecimal)\s+("[^"]*"|[^\s;]+)`,
	`(?i)\b(?:ftp|pasvftp|tftp|scp|sftp|https?)://[^/\s:@"]+:([^@\s"]+)@`,
}
