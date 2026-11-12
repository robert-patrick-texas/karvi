package askpass

import (
	"bytes"
	"io"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
	"github.com/robert-patrick-texas/karvi/internal/secrets"
	"github.com/robert-patrick-texas/karvi/internal/testsocket"
)

// TestBrokerAsksTheOperatorInALogin: in a login (Hooks.Operator) the broker
// still answers the username and the password itself, each once, and gives
// the operator, any number of times, a change prompt, a prompt no list
// matches, and a field already answered, the prompt as OpenSSH gave it;
// the line typed is the answer, then wiped (each earlier line checked, in
// the broker's goroutine, when the operator is next asked). OpenSSH's own
// questions stay refused, unasked; nothing ends the connection.
func TestBrokerAsksTheOperatorInALogin(t *testing.T) {
	material := secrets.NewMaterial("operator", "login-secret", "enable-secret")
	defer material.Destroy()
	var mu sync.Mutex
	var asked, unwiped []string
	var typed [][]byte
	b, err := Start(testsocket.Dir(t), material, Hooks{
		End: func() { t.Error("the End hook was called") },
		Operator: func(prompt string) ([]byte, error) {
			mu.Lock()
			defer mu.Unlock()
			for i, line := range typed {
				if strings.Trim(string(line), "\x00") != "" {
					unwiped = append(unwiped, strconv.Itoa(i+1)+": "+string(line))
				}
			}
			asked = append(asked, prompt)
			line := []byte("typed-" + strconv.Itoa(len(asked)))
			typed = append(typed, line)
			return line, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	rows := []struct{ prompt, secret, err string }{
		{"Enter passphrase for key '/home/operator/.ssh/id_ed25519': ", "", "askpass_prompt_unrecognized"},
		{"(operator@router1) Username: ", "operator", ""},
		{"(operator@router1) Enter PASSCODE: ", "login-secret", ""},
		{"(operator@router1) Enter new PIN: ", "typed-1", ""},
		{"(operator@router1) Please re-enter new PIN: ", "typed-2", ""},
		{"(operator@router1) Enter PASSCODE: ", "typed-3", ""},
		{"operator@router1's password: ", "typed-4", ""},
		{"(operator@router1) login as: ", "typed-5", ""},
		{"(operator@router1) Select a role [1-3]: ", "typed-6", ""},
		{"(operator@router1) Re-enter PIN: ", "typed-7", ""},
	}
	var want []string
	for _, c := range rows {
		if strings.HasPrefix(c.secret, "typed-") {
			want = append(want, c.prompt)
		}
		if got := ask(t, b, "", c.prompt); string(got.Secret) != c.secret || got.Error != c.err {
			t.Errorf("%q: %+v, want %q %q", c.prompt, got, c.secret, c.err)
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if strings.Join(asked, "|") != strings.Join(want, "|") {
		t.Errorf("the operator was asked %q, want %q", asked, want)
	}
	if len(unwiped) != 0 {
		t.Errorf("answers not wiped: %q", unwiped)
	}
	if err := b.Err(); err != nil {
		t.Errorf("Err() = %v, want nil", err)
	}
	if !hasLoginEnvironment(b) {
		t.Errorf("Environment() = %q, want %s=1", b.Environment(), LoginEnv)
	}
}

// TestBrokerSendsAnEmptyOperatorAnswer: an empty line from the operator
// (Ctrl-D on an empty line) is an empty answer, not a refusal, and the
// broker serves on.
func TestBrokerSendsAnEmptyOperatorAnswer(t *testing.T) {
	material := secrets.NewMaterial("operator", "login-secret", "")
	defer material.Destroy()
	b, err := Start(testsocket.Dir(t), material, Hooks{
		End:      func() { t.Error("the End hook was called") },
		Operator: func(string) ([]byte, error) { return []byte{}, nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	if got := ask(t, b, "", "(operator@router1) Enter new PIN: "); got.Error != "" || len(got.Secret) != 0 {
		t.Errorf("an empty line: %+v, want an empty answer", got)
	}
	if got := ask(t, b, "", "(operator@router1) Password: "); got.Error != "" || string(got.Secret) != "login-secret" {
		t.Errorf("after an empty line: %+v, want the password", got)
	}
}

// TestBrokerEndsAtTheOperatorsError: an Operator error other than
// ErrNoOperator (Ctrl-C at the prompt, credential_prompt_interrupted) ends
// the connection as a change prompt does without an operator: the End hook
// has returned before the refusal goes back, the error is Err, and the
// broker answers nothing more.
func TestBrokerEndsAtTheOperatorsError(t *testing.T) {
	material := secrets.NewMaterial("operator", "login-secret", "")
	defer material.Destroy()
	var hooked atomic.Int32
	b, err := Start(testsocket.Dir(t), material, Hooks{
		End: func() {
			time.Sleep(100 * time.Millisecond)
			hooked.Add(1)
		},
		Operator: func(string) ([]byte, error) {
			return nil, errorcodes.Errorf("credential_prompt_interrupted", "interrupted at the device's prompt")
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	got := ask(t, b, "", "(operator@router1) Enter new PIN: ")
	if hooked.Load() != 1 {
		t.Errorf("the End hook had run %d times when the refusal arrived, want once", hooked.Load())
	}
	if got.Error != "credential_prompt_interrupted" || len(got.Secret) != 0 {
		t.Errorf("%+v, want credential_prompt_interrupted", got)
	}
	if err := b.Err(); errorcodes.Message(err) != "credential_prompt_interrupted: interrupted at the device's prompt" {
		t.Errorf("Err() = %v", err)
	}
	if answersAgain(b) {
		t.Error("the broker serves on after the operator's error")
	}
}

// TestBrokerWithoutATerminalAnswersAsACommandSession: an Operator answering
// ErrNoOperator (a login with no controlling terminal) leaves each prompt to
// the broker's own rule: a change prompt and a second ask end the
// connection, a prompt no list matches is refused and the broker serves on.
func TestBrokerWithoutATerminalAnswersAsACommandSession(t *testing.T) {
	material := secrets.NewMaterial("operator", "login-secret", "")
	defer material.Destroy()
	for _, c := range []struct {
		prompts []string
		code    string
		ended   string
	}{
		{[]string{"(operator@router1) Enter new PIN: "}, "authentication_change_requested", `authentication_change_requested: the device asks to change a secret: "Enter new PIN: "`},
		{[]string{"(operator@router1) Password: ", "(operator@router1) Password: "}, "authentication_failed", "authentication_failed: device refused password"},
		{[]string{"(operator@router1) Select a role [1-3]: "}, "askpass_prompt_unrecognized", ""},
	} {
		var asked, hooked atomic.Int32
		b, err := Start(testsocket.Dir(t), material, Hooks{
			End:      func() { hooked.Add(1) },
			Operator: func(string) ([]byte, error) { asked.Add(1); return nil, ErrNoOperator },
		})
		if err != nil {
			t.Fatal(err)
		}
		var got response
		for _, prompt := range c.prompts {
			got = ask(t, b, "", prompt)
		}
		if got.Error != c.code {
			t.Errorf("%q: %+v, want %s", c.prompts, got, c.code)
		}
		if asked.Load() != 1 {
			t.Errorf("%q: the operator was asked %d times, want once", c.prompts, asked.Load())
		}
		if got := errorcodes.Message(b.Err()); got != c.ended {
			t.Errorf("%q: Err() = %q, want %q", c.prompts, got, c.ended)
		}
		if want := btoi(c.ended != ""); int(hooked.Load()) != want {
			t.Errorf("%q: the End hook was called %d times, want %d", c.prompts, hooked.Load(), want)
		}
		if ended := !answersAgain(b); ended != (c.ended != "") {
			t.Errorf("%q: the broker ended %t, want %t", c.prompts, ended, c.ended != "")
		}
		b.Close()
	}
}

// TestALoginsOperatorHasNoExchangeBound: in a login an operator's answer
// takes as long as the operator types, on both sides: the broker lifts the
// exchange's bound while it asks, and the helper, told by LoginEnv, waits;
// outside a login the helper gives up at the bound, and in a login the
// broker still reads each request within it.
func TestALoginsOperatorHasNoExchangeBound(t *testing.T) {
	defer func(d time.Duration) { exchangeTimeout = d }(exchangeTimeout)
	exchangeTimeout = 100 * time.Millisecond
	material := secrets.NewMaterial("operator", "login-secret", "")
	defer material.Destroy()
	b, err := Start(testsocket.Dir(t), material, Hooks{
		Operator: func(string) ([]byte, error) {
			time.Sleep(4 * exchangeTimeout)
			return []byte("731904"), nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	socket, token := brokerSocket(b), brokerToken(b)
	var out bytes.Buffer
	if code := helper(socket, token, true, "(operator@router1) Enter new PIN: ", &out); code != 0 || out.String() != "731904\n" {
		t.Errorf("in a login: exit %d, %q, want 0 and the line typed", code, out.String())
	}
	out.Reset()
	if code := helper(socket, token, false, "(operator@router1) Enter new PIN: ", &out); code != 5 || out.Len() != 0 {
		t.Errorf("outside a login: exit %d, %q, want 5 and nothing", code, out.String())
	}
	// The broker, still asking for the helper that gave up, then reads the
	// next request within the bound: one that sends nothing ends it.
	time.Sleep(4 * exchangeTimeout)
	conn, err := net.DialTimeout("unix", socket, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(10 * time.Second))
	if _, err := io.ReadAll(conn); err != nil {
		t.Fatal(err)
	}
	if got := errorcodes.Of(b.Err()); got != "askpass_request_invalid" {
		t.Errorf("a request sending nothing in a login: Err() = %v, want askpass_request_invalid", b.Err())
	}
}

// TestHelperMainReadsTheLoginEnvironment: the helper process waits on a
// slow operator's answer when LoginEnv is 1, and gives up at the
// exchange's bound otherwise.
func TestHelperMainReadsTheLoginEnvironment(t *testing.T) {
	defer func(d time.Duration) { exchangeTimeout = d }(exchangeTimeout)
	exchangeTimeout = 100 * time.Millisecond
	material := secrets.NewMaterial("operator", "login-secret", "")
	defer material.Destroy()
	b, err := Start(testsocket.Dir(t), material, Hooks{
		Operator: func(string) ([]byte, error) {
			time.Sleep(4 * exchangeTimeout)
			return []byte("731904"), nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	t.Setenv(SocketEnv, brokerSocket(b))
	t.Setenv(TokenEnv, brokerToken(b))
	for _, c := range []struct {
		login string
		code  int
		out   string
	}{{"1", 0, "731904\n"}, {"", 5, ""}, {"yes", 5, ""}} {
		t.Setenv(LoginEnv, c.login)
		r, w, err := os.Pipe()
		if err != nil {
			t.Fatal(err)
		}
		stdout := os.Stdout
		os.Stdout = w
		code := HelperMain([]string{"(operator@router1) Enter new PIN: "})
		os.Stdout = stdout
		w.Close()
		got, _ := io.ReadAll(r)
		r.Close()
		if code != c.code || string(got) != c.out {
			t.Errorf("%s=%q: exit %d, %q, want %d, %q", LoginEnv, c.login, code, got, c.code, c.out)
		}
		time.Sleep(4 * exchangeTimeout) // the broker done with an abandoned answer
	}
}

// TestEnvironmentNamesALoginOnlyWithAnOperator: LoginEnv is in the helper's
// environment only where the broker has an operator to ask.
func TestEnvironmentNamesALoginOnlyWithAnOperator(t *testing.T) {
	material := secrets.NewMaterial("operator", "login-secret", "")
	defer material.Destroy()
	b, err := Start(testsocket.Dir(t), material, Hooks{})
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	if hasLoginEnvironment(b) {
		t.Errorf("Environment() = %q, want no %s", b.Environment(), LoginEnv)
	}
}

func hasLoginEnvironment(b *Broker) bool {
	for _, value := range b.Environment() {
		if value == LoginEnv+"=1" {
			return true
		}
	}
	return false
}

// brokerToken is b's token, as its environment names it.
func brokerToken(b *Broker) string {
	for _, value := range b.Environment() {
		if key, val, _ := strings.Cut(value, "="); key == TokenEnv {
			return val
		}
	}
	return ""
}

func btoi(v bool) int {
	if v {
		return 1
	}
	return 0
}
