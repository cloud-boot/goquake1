// Copyright (c) 1996-1997 Id Software, Inc.
// Copyright (c) 2026 the cloud-boot/goquake1 authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package cmd

import "errors"

// Handler is the function signature every registered command exposes.
// args[0] is the command name as typed (post-tokenisation), args[1:]
// are the parsed parameters. tyrquake: xcommand_t.
type Handler func(args []string)

// MaxAliasNameLen mirrors MAX_ALIAS_NAME from tyrquake's cmd.c -- an
// alias name longer than this is rejected by AddAlias.
const MaxAliasNameLen = 32

// maxAliasDepth caps recursive alias expansion so a self-referential
// alias (alias foo "foo") errors out instead of hanging. tyrquake has
// no equivalent guard (it relies on the buffer's bounded size to break
// the loop); the port adds one because Registry.Execute runs aliases
// inline rather than re-queueing them through a Buffer.
const maxAliasDepth = 32

// ErrAliasRecursion is returned by Registry.Execute when an alias
// expansion chain exceeds maxAliasDepth. Not produced by upstream
// tyrquake -- see [maxAliasDepth] for the rationale.
var ErrAliasRecursion = errors.New("cmd: alias expansion exceeds maximum depth")

// Buffer is the deferred-execution queue. Callers Add or Insert text
// (configs, console lines, network commands); Execute drains it
// line-by-line through a Registry. tyrquake: cmd_text (sizebuf_t) plus
// the Cbuf_* functions that operate on it.
type Buffer struct {
	text []byte
	// wait is set by the "wait" command (Cmd_Wait_f) to defer the
	// remainder of the buffer until the next Execute call. Mirrors
	// the cmd_wait static in cmd.c.
	wait bool
}

// NewBuffer returns an empty Buffer ready for Add/Insert. tyrquake:
// Cbuf_Init (we drop the fixed 16 KiB pre-allocation; Go slices grow).
func NewBuffer() *Buffer {
	return &Buffer{}
}

// Add appends text to the buffer's tail. tyrquake: Cbuf_AddText (the
// printf-formatting overload is dropped; callers format upfront).
func (b *Buffer) Add(text string) {
	b.text = append(b.text, text...)
}

// Insert prepends text + '\n' to the buffer's head so it executes
// before any text already queued. tyrquake: Cbuf_InsertText.
func (b *Buffer) Insert(text string) {
	// Build [text + '\n' + existing] in one allocation so the buffer
	// doesn't churn when an alias body is much larger than the queue.
	combined := make([]byte, 0, len(text)+1+len(b.text))
	combined = append(combined, text...)
	combined = append(combined, '\n')
	combined = append(combined, b.text...)
	b.text = combined
}

// Wait marks the buffer so the current Execute call returns after the
// in-flight line and resumes on the next Execute. tyrquake: Cmd_Wait_f
// (here exposed as a method rather than a registered command, so
// callers wire it in if they want it: reg.Add("wait", buf.WaitHandler)).
func (b *Buffer) Wait() {
	b.wait = true
}

// WaitHandler is the Handler that hooks Wait into a Registry. tyrquake:
// Cmd_Wait_f.
func (b *Buffer) WaitHandler(args []string) {
	_ = args
	b.Wait()
}

// Len returns the number of bytes currently queued. tyrquake:
// cmd_text.cursize.
func (b *Buffer) Len() int { return len(b.text) }

// Execute drains the buffer one line at a time, dispatching each line
// through reg. Lines split on the first unquoted ';' or '\n'. Returns
// the first non-nil error a Registry.Execute call produces (alias
// recursion); the buffer is preserved from the failing line onward so
// the caller can inspect or reset it. tyrquake: Cbuf_Execute.
func (b *Buffer) Execute(reg *Registry) error {
	for len(b.text) > 0 {
		line, rest := splitFirstLine(b.text)
		b.text = rest
		if err := reg.Execute(line); err != nil {
			return err
		}
		if b.wait {
			b.wait = false
			return nil
		}
	}
	return nil
}

// splitFirstLine returns (line, remainder) where line is everything up
// to the first unquoted ';' or any '\n' (newlines break unconditionally
// -- the quote check only gates ';'). The separator itself is consumed.
// tyrquake: the inner loop of Cbuf_Execute.
func splitFirstLine(data []byte) (line string, rest []byte) {
	quotes := 0
	for i := 0; i < len(data); i++ {
		c := data[i]
		if c == '"' {
			quotes++
			continue
		}
		if c == '\n' {
			return string(data[:i]), data[i+1:]
		}
		if quotes%2 == 0 && c == ';' {
			return string(data[:i]), data[i+1:]
		}
	}
	return string(data), nil
}

// Registry owns the named-Handler table and the alias table. tyrquake
// keeps these in two separate stree_t globals (cmd_tree, cmdalias_tree);
// we bundle them so a single lookup-site sees both name spaces.
type Registry struct {
	commands map[string]Handler
	aliases  map[string]string
	// buf is the buffer aliases insert into. When nil, alias bodies
	// execute inline via the recursion-bounded fast path; when set
	// (typically by Buffer.Execute), alias bodies are queued at the
	// head of the buffer so the next loop iteration runs them --
	// matching tyrquake's Cbuf_InsertText behaviour.
	buf *Buffer
	// depth tracks in-flight alias recursion for the inline path.
	depth int
}

// New returns an empty Registry. tyrquake: the implicit init from
// DECLARE_STREE_ROOT.
func New() *Registry {
	return &Registry{
		commands: make(map[string]Handler),
		aliases:  make(map[string]string),
	}
}

// Add registers a command handler. Re-registering an existing name is
// a no-op -- mirrors tyrquake's "already defined" path (which logs and
// returns without overwriting). tyrquake: Cmd_AddCommand.
func (r *Registry) Add(name string, fn Handler) {
	if _, ok := r.commands[name]; ok {
		return
	}
	r.commands[name] = fn
}

// Remove drops a command handler. Missing names are silently ignored
// (tyrquake's stree_t has no public removal; the engine never removes
// commands at runtime, but the port exposes it for test cleanup).
func (r *Registry) Remove(name string) {
	delete(r.commands, name)
}

// Exists reports whether a command is registered. tyrquake: Cmd_Exists.
func (r *Registry) Exists(name string) bool {
	_, ok := r.commands[name]
	return ok
}

// AddAlias defines or overwrites an alias. The body executes verbatim
// (as if typed) every time the alias name is invoked. Returns false if
// the name exceeds MaxAliasNameLen. tyrquake: Cmd_Alias_f, which both
// overwrites and frees the previous value -- the Go map assignment
// handles both.
func (r *Registry) AddAlias(name, body string) bool {
	if len(name) >= MaxAliasNameLen {
		return false
	}
	r.aliases[name] = body
	return true
}

// AliasExists reports whether name is a defined alias. tyrquake:
// Cmd_Alias_Exists.
func (r *Registry) AliasExists(name string) bool {
	_, ok := r.aliases[name]
	return ok
}

// AttachBuffer wires a Buffer into the Registry so alias expansions
// queue into it (Cbuf_InsertText) rather than running inline. Pass nil
// to detach. tyrquake: implicit -- cmd_text and the cmd module share a
// translation unit.
func (r *Registry) AttachBuffer(b *Buffer) {
	r.buf = b
}

// Execute tokenises line and dispatches the first token: as a command
// if registered, as an alias if defined, otherwise silently dropped
// (tyrquake logs "Unknown command" gated on cl_warncmd -- the Go port
// defers warnings to the host layer).
//
// Empty lines and comment-only lines no-op. tyrquake: Cmd_ExecuteString.
func (r *Registry) Execute(line string) error {
	args := Tokenize(line)
	if len(args) == 0 {
		return nil
	}
	name := args[0]
	if fn, ok := r.commands[name]; ok {
		fn(args)
		return nil
	}
	if body, ok := r.aliases[name]; ok {
		return r.expandAlias(body)
	}
	return nil
}

// expandAlias runs an alias body. With a buffer attached the body
// queues at the head and the surrounding Buffer.Execute loop handles
// it (no recursion in this call). Without one, the body executes
// line-by-line right here, bounded by maxAliasDepth.
func (r *Registry) expandAlias(body string) error {
	if r.buf != nil {
		r.buf.Insert(body)
		return nil
	}
	if r.depth >= maxAliasDepth {
		return ErrAliasRecursion
	}
	r.depth++
	defer func() { r.depth-- }()
	data := []byte(body)
	for len(data) > 0 {
		line, rest := splitFirstLine(data)
		data = rest
		if err := r.Execute(line); err != nil {
			return err
		}
	}
	return nil
}

// Tokenize splits a line into space-delimited tokens, honouring
// double-quoted strings (which may contain whitespace) and stopping at
// a "//" line comment. tyrquake: Cmd_TokenizeString driving COM_Parse,
// fused here since this package is COM_Parse's only consumer.
//
// An unterminated quoted string runs to end-of-input and yields one
// token containing every character up to (but not including) the
// missing closing quote -- the same behaviour as upstream COM_Parse,
// which exits its inner loop on the NUL terminator.
func Tokenize(line string) []string {
	var args []string
	data := line
	for {
		// Skip whitespace up to (but not past) any newline -- tyrquake
		// uses '\n' to separate commands inside the buffer, but at the
		// per-line API a newline is just whitespace.
		for len(data) > 0 && data[0] <= ' ' {
			data = data[1:]
		}
		if len(data) == 0 {
			return args
		}
		// Line-comment: "//" ends the line.
		if len(data) >= 2 && data[0] == '/' && data[1] == '/' {
			return args
		}
		tok, rest := parseToken(data)
		data = rest
		args = append(args, tok)
	}
}

// parseToken extracts a single token from data and returns it plus the
// remaining input. tyrquake: COM_Parse (the non-split_single_chars
// variant -- console lines don't break on { } ( ) ' :).
func parseToken(data string) (token, rest string) {
	if data[0] == '"' {
		// Quoted string: consume until the closing quote or EOF.
		data = data[1:]
		end := 0
		for end < len(data) && data[end] != '"' {
			end++
		}
		token = data[:end]
		if end < len(data) {
			// Skip the closing quote.
			return token, data[end+1:]
		}
		return token, ""
	}
	// Regular word: run until whitespace.
	end := 0
	for end < len(data) && data[end] > ' ' {
		end++
	}
	return data[:end], data[end:]
}
