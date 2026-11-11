//go:build dm

package database

import (
	"encoding/base64"
	"errors"
	"io"
	"net"
	"strconv"
	"strings"

	"dbmcp.local/server/internal/model"
	"gitee.com/chunanyong/dm"
)

const DMAvailable = true

func nativeValue(v any, limit int) (any, bool, error) {
	switch x := v.(type) {
	case *dm.DmClob:
		length, err := x.GetLength()
		if err != nil || length < 0 || length > int64(limit) {
			return nil, true, errors.New("DM CLOB exceeds the result limit or cannot be read")
		}
		if length == 0 {
			return "", true, nil
		}
		s, err := x.ReadString(1, int(length))
		if err != nil && err != io.EOF {
			return nil, true, errors.New("cannot read DM CLOB")
		}
		if len(s) > limit {
			return nil, true, errors.New("DM CLOB exceeds the result byte limit")
		}
		return s, true, nil
	case *dm.DmBlob:
		length, err := x.GetLength()
		if err != nil || length < 0 || length > int64(limit) {
			return nil, true, errors.New("DM BLOB exceeds the result limit or cannot be read")
		}
		if length == 0 {
			return "base64:", true, nil
		}
		b := make([]byte, int(length))
		n, err := x.ReadAt(1, b)
		if err != nil && err != io.EOF {
			return nil, true, errors.New("cannot read DM BLOB")
		}
		return "base64:" + base64.StdEncoding.EncodeToString(b[:n]), true, nil
	}
	return nil, false, nil
}

func dmDSN(i model.Instance) (string, string, error) {
	c := i.Connection
	// This native release splits on the LAST '?' and '@' and does not URL-decode
	// user information. url.UserPassword would corrupt ordinary special passwords.
	// The final delimiters below keep password punctuation outside the query.
	if strings.Contains(c.Username, ":") || strings.Contains(DefaultSchema(i), "/") {
		return "", "", errors.New("native DM driver cannot represent ':' in usernames or '/' in schemas")
	}
	if c.TLS != "disable" {
		return "", "", errors.New("this native DM release cannot enforce client TLS or verify the server certificate; use an explicitly protected local/tunneled connection with TLS disabled")
	}
	dsn := "dm://" + c.Username + ":" + c.Password + "@" + net.JoinHostPort(c.Host, strconv.Itoa(c.Port)) + "/" + DefaultSchema(i) + "?connectTimeout=10000&socketTimeout=300000"
	return "dm", dsn, nil
}
