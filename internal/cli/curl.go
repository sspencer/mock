package cli

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
)

// curlCommands splits text into curl commands: each starts at a "curl"
// word, and backslash-newline continues a line. Text before the first
// command (a shell prompt, comments) is ignored.
func curlCommands(text string) ([][]string, error) {
	words, err := shellWords(text)
	if err != nil {
		return nil, err
	}
	var commands [][]string
	afterPrompt := false
	for _, word := range words {
		// A copied shell prompt ("$ curl ...") does not hide the command.
		if word.first && (word.text == "$" || word.text == "%" || word.text == ">") {
			afterPrompt = true
			continue
		}
		first := word.first || afterPrompt
		afterPrompt = false
		if word.text == "curl" && first {
			commands = append(commands, nil)
			continue
		}
		if len(commands) > 0 && !word.separator {
			commands[len(commands)-1] = append(commands[len(commands)-1], word.text)
		}
	}
	return commands, nil
}

// shellWord is one word of shell text. first marks a word that begins a
// command (at the start of a line or after ;, &&, || or |); separator marks
// those operators themselves.
type shellWord struct {
	text             string
	first, separator bool
}

// shellWords splits text into words the way a POSIX shell quotes them:
// '...' is literal, "..." honors \" \\ \$ and \`, $'...' honors C escapes,
// and a backslash outside quotes escapes the next character (a newline
// after it continues the line). Variables are not expanded.
func shellWords(text string) ([]shellWord, error) {
	var words []shellWord
	var cur strings.Builder
	inWord, atStart := false, true
	flush := func() {
		if inWord {
			words = append(words, shellWord{text: cur.String(), first: atStart})
			atStart = false
		}
		cur.Reset()
		inWord = false
	}
	for i := 0; i < len(text); i++ {
		c := text[i]
		switch {
		case c == '\\' && i+1 < len(text):
			i++
			if text[i] == '\n' {
				continue
			}
			if text[i] == '\r' && i+1 < len(text) && text[i+1] == '\n' {
				i++
				continue
			}
			cur.WriteByte(text[i])
			inWord = true
		case c == '\'':
			end := strings.IndexByte(text[i+1:], '\'')
			if end < 0 {
				return nil, fmt.Errorf("unterminated ' quote")
			}
			cur.WriteString(text[i+1 : i+1+end])
			i += end + 1
			inWord = true
		case c == '$' && i+1 < len(text) && text[i+1] == '\'':
			n, err := ansiCQuote(text[i+2:], &cur)
			if err != nil {
				return nil, err
			}
			i += n + 1
			inWord = true
		case c == '"':
			i++
			for ; i < len(text) && text[i] != '"'; i++ {
				if text[i] == '\\' && i+1 < len(text) && strings.IndexByte("\"\\$`\n", text[i+1]) >= 0 {
					i++
					if text[i] == '\n' {
						continue
					}
				}
				cur.WriteByte(text[i])
			}
			if i >= len(text) {
				return nil, fmt.Errorf(`unterminated " quote`)
			}
			inWord = true
		case c == '#' && !inWord:
			for i < len(text) && text[i] != '\n' {
				i++
			}
			i--
		case c == '\n' || c == ';' || c == '|' || c == '&':
			flush()
			if c != '\n' {
				words = append(words, shellWord{text: string(c), separator: true})
			}
			atStart = true
		case c == ' ' || c == '\t' || c == '\r':
			flush()
		default:
			cur.WriteByte(c)
			inWord = true
		}
	}
	flush()
	return words, nil
}

// ansiCQuote reads the body of a $'...' word up to its closing quote,
// writing the decoded text, and returns how many bytes it consumed
// (including the quote).
func ansiCQuote(text string, out *strings.Builder) (int, error) {
	escapes := map[byte]byte{'n': '\n', 't': '\t', 'r': '\r', '\\': '\\', '\'': '\'', '"': '"', 'a': 7, 'b': 8, 'e': 27, 'f': 12, 'v': 11}
	for i := 0; i < len(text); i++ {
		switch c := text[i]; {
		case c == '\'':
			return i + 1, nil
		case c == '\\' && i+1 < len(text):
			i++
			if r, ok := escapes[text[i]]; ok {
				out.WriteByte(r)
			} else {
				out.WriteByte('\\')
				out.WriteByte(text[i])
			}
		default:
			out.WriteByte(c)
		}
	}
	return 0, fmt.Errorf("unterminated $' quote")
}

// curlArgFlags are curl options that take a value mock import does not use;
// their value is skipped.
var curlArgFlags = map[string]bool{
	"-o": true, "--output": true, "-w": true, "--write-out": true, "-m": true, "--max-time": true,
	"--connect-timeout": true, "-x": true, "--proxy": true, "-U": true, "--proxy-user": true,
	"--cacert": true, "--capath": true, "-E": true, "--cert": true, "--key": true, "--cert-type": true,
	"--retry": true, "--retry-delay": true, "--retry-max-time": true, "-c": true, "--cookie-jar": true,
	"-D": true, "--dump-header": true, "--resolve": true, "--connect-to": true, "--limit-rate": true,
	"-r": true, "--range": true, "-T": true, "--upload-file": true, "-K": true, "--config": true,
	"--interface": true, "--max-redirs": true, "-z": true, "--time-cond": true, "--trace": true,
	"--trace-ascii": true, "--stderr": true, "--unix-socket": true, "--abstract-unix-socket": true,
	"--aws-sigv4": true, "--oauth2-bearer": true,
}

// curlExchange turns one curl command's arguments into an exchange with a
// placeholder response (curl commands carry no response).
func curlExchange(args []string) (exchange, error) {
	headers := http.Header{}
	var method, rawURL string
	var data []string
	get, jsonBody := false, false
	for i := 0; i < len(args); i++ {
		arg := args[i]
		value := func() (string, error) {
			if i+1 >= len(args) {
				return "", fmt.Errorf("%s needs a value", arg)
			}
			i++
			return args[i], nil
		}
		// --name=value is not curl syntax, but -XPOST and -HName: v are.
		name := arg
		inline := ""
		if len(arg) > 2 && arg[0] == '-' && arg[1] != '-' && strings.IndexByte("XHdbAeu", arg[1]) >= 0 {
			name, inline = arg[:2], arg[2:]
			value = func() (string, error) { return inline, nil }
		}
		var err error
		var v string
		switch name {
		case "-X", "--request":
			v, err = value()
			method = strings.ToUpper(v)
		case "-H", "--header":
			v, err = value()
			if key, val, ok := strings.Cut(v, ":"); ok && strings.TrimSpace(key) != "" {
				headers.Add(strings.TrimSpace(key), strings.TrimSpace(val))
			}
		case "-d", "--data", "--data-raw", "--data-binary", "--data-ascii":
			v, err = value()
			if strings.HasPrefix(v, "@") && name != "--data-raw" {
				v, err = readCurlFile(v[1:], name == "--data-binary")
			}
			data = append(data, v)
		case "--data-urlencode":
			v, err = value()
			data = append(data, curlURLEncode(v))
		case "--json":
			v, err = value()
			if strings.HasPrefix(v, "@") {
				v, err = readCurlFile(v[1:], true)
			}
			data = append(data, v)
			jsonBody = true
		case "-G", "--get":
			get = true
		case "-A", "--user-agent":
			v, err = value()
			headers.Set("User-Agent", v)
		case "-e", "--referer":
			v, err = value()
			headers.Set("Referer", v)
		case "-b", "--cookie":
			v, err = value()
			if strings.Contains(v, "=") {
				headers.Add("Cookie", v)
			}
		case "-u", "--user":
			v, err = value()
			headers.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(v)))
		case "--oauth2-bearer":
			v, err = value()
			headers.Set("Authorization", "Bearer "+v)
		case "-I", "--head":
			method = http.MethodHead
		case "--url":
			rawURL, err = value()
		default:
			switch {
			case curlArgFlags[arg]:
				_, err = value()
			case strings.HasPrefix(arg, "-") && arg != "-":
				// A flag without a value, such as -s, -L or --compressed.
			case rawURL == "":
				rawURL = arg
			}
		}
		if err != nil {
			return exchange{}, err
		}
	}
	if rawURL == "" {
		return exchange{}, fmt.Errorf("no URL")
	}
	if !strings.Contains(rawURL, "://") {
		rawURL = "http://" + rawURL
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return exchange{}, err
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return exchange{}, fmt.Errorf("%s is not an http URL", rawURL)
	}
	body := strings.Join(data, "&")
	if jsonBody {
		body = strings.Join(data, "")
		if headers.Get("Content-Type") == "" {
			headers.Set("Content-Type", "application/json")
		}
		if headers.Get("Accept") == "" {
			headers.Set("Accept", "application/json")
		}
	}
	if get && len(data) > 0 {
		if u.RawQuery != "" {
			u.RawQuery += "&"
		}
		u.RawQuery += body
		body = ""
	}
	switch {
	case method != "":
	case body != "":
		method = http.MethodPost
	default:
		method = http.MethodGet
	}
	if body != "" && headers.Get("Content-Type") == "" {
		headers.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	target := u.EscapedPath()
	if target == "" {
		target = "/"
	}
	if u.RawQuery != "" {
		target += "?" + u.RawQuery
	}
	return exchange{Method: method, Target: target, RequestHeaders: headers, RequestBody: body, host: u.Host}, nil
}

// curlURLEncode encodes a --data-urlencode value: "content", "=content",
// "name=content", or the file forms "@file" and "name@file".
func curlURLEncode(v string) string {
	if name, content, ok := strings.Cut(v, "="); ok {
		if name == "" {
			return url.QueryEscape(content)
		}
		return name + "=" + url.QueryEscape(content)
	}
	if name, file, ok := strings.Cut(v, "@"); ok {
		content, err := readCurlFile(file, true)
		if err != nil {
			content = ""
		}
		if name == "" {
			return url.QueryEscape(content)
		}
		return name + "=" + url.QueryEscape(content)
	}
	return url.QueryEscape(v)
}

// readCurlFile reads the file named by a @file data argument. Without raw,
// curl drops carriage returns and newlines, as -d does.
func readCurlFile(name string, raw bool) (string, error) {
	if name == "-" {
		return "", fmt.Errorf("@- (data from stdin) is not supported")
	}
	data, err := os.ReadFile(name)
	if err != nil {
		return "", err
	}
	if raw {
		return string(data), nil
	}
	return strings.NewReplacer("\r", "", "\n", "").Replace(string(data)), nil
}

// curlExchanges converts every curl command in text, keeping those whose
// host and path match the filters.
func curlExchanges(text, host, prefix string) ([]exchange, int, error) {
	commands, err := curlCommands(text)
	if err != nil {
		return nil, 0, err
	}
	var exchanges []exchange
	skipped := 0
	for n, args := range commands {
		x, err := curlExchange(args)
		if err != nil {
			return nil, 0, fmt.Errorf("curl command %d: %w", n+1, err)
		}
		hostname, _, _ := strings.Cut(x.host, ":")
		path, _, _ := strings.Cut(x.Target, "?")
		if host != "" && !strings.EqualFold(x.host, host) && !strings.EqualFold(hostname, host) || prefix != "" && !strings.HasPrefix(path, prefix) {
			skipped++
			continue
		}
		exchanges = append(exchanges, x)
	}
	return exchanges, skipped, nil
}
