package pq

import (
	"bufio"
	"context"
	"crypto/md5"
	"crypto/tls"
	"database/sql/driver"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"gitee.com/opengauss/openGauss-connector-go-pq/oid"
)

// Common error types
var (
	ErrNotSupported              = errors.New("pq: Unsupported command")
	ErrInFailedTransaction       = errors.New("pq: Could not complete operation in a failed transaction")
	ErrSSLNotSupported           = errors.New("pq: SSL is not enabled on the server")
	ErrSSLKeyHasWorldPermissions = errors.New("pq: Private key file has group or world access. Permissions should be u=rw (0600) or less")
	ErrCouldNotDetectUsername    = errors.New("pq: Could not detect default username. Please provide one explicitly")

	errUnexpectedReady = errors.New("unexpected ReadyForQuery")
	errNoRowsAffected  = errors.New("no RowsAffected available after the empty statement")
	errNoLastInsertID  = errors.New("no LastInsertId available after the empty statement")
)

const (
	AuthReqOk          = 0
	AUTH_REQ_KRB4      = 1
	AUTH_REQ_KRB5      = 2
	AuthReqPassword    = 3
	AUTH_REQ_CRYPT     = 4
	AuthReqMd5         = 5
	AUTH_REQ_SCM       = 6
	AuthReqGss         = 7
	AuthReqGssContinue = 8
	AUTH_REQ_SSPI      = 9
	AuthReqSha256      = 10
	AuthReqMd5Sha256   = 11
	AuthReqSm3         = 13

	PlainPassword  = 0
	Md5Password    = 1
	Sha256Password = 2

	Sm3Password = 3
)

type parameterStatus struct {
	// server version in the same format as server_version_num, or 0 if
	// unavailable
	serverVersion int

	// the current location based on the TimeZone value of the session, if
	// available
	currentLocation *time.Location
}

type transactionStatus byte

const (
	txnStatusIdle                transactionStatus = 'I'
	txnStatusIdleInTransaction   transactionStatus = 'T'
	txnStatusInFailedTransaction transactionStatus = 'E'
)

func (s transactionStatus) String() string {
	switch s {
	case txnStatusIdle:
		return "idle"
	case txnStatusIdleInTransaction:
		return "idle in transaction"
	case txnStatusInFailedTransaction:
		return "in a failed transaction"
	default:
		errorf("unknown transactionStatus %d", s)
	}

	panic("not reached")
}

type conn struct {
	c   net.Conn
	buf *bufio.Reader

	logger         Logger
	logLevel       LogLevel
	config         *Config
	fallbackConfig *FallbackConfig
	namei          int
	scratch        [512]byte
	txnStatus      transactionStatus
	txnFinish      func()
	// Save connection arguments to use during CancelRequest.
	dialer Dialer

	// Cancellation key data for use with CancelRequest messages.
	processID int
	secretKey int

	parameterStatus parameterStatus

	saveMessageType   byte
	saveMessageBuffer []byte

	// If true, this connection is bad and all public-facing functions should
	// return ErrBadConn.
	bad *atomic.Value

	// If set, this connection should never use the binary format when
	// receiving query results from prepared statements.  Only provided for
	// debugging.
	disablePreparedBinaryResult bool

	// Whether to always send []byte parameters over as binary.  Enables single
	// round-trip mode for non-prepared Query calls.
	binaryParameters bool

	// If true this connection is in the middle of a COPY
	inCopy bool

	// If not nil, notices will be synchronously sent here
	noticeHandler func(*Error)

	// If not nil, notifications will be synchronously sent here
	notificationHandler func(*Notification)

	// GSSAPI context
	gss GSS
}

func (cn *conn) shouldLog(lvl LogLevel) bool {
	return cn.logger != nil && cn.logLevel >= lvl
}
func (cn *conn) log(ctx context.Context, lvl LogLevel, msg string, data map[string]interface{}) {
	if !cn.shouldLog(lvl) {
		return
	}
	if data == nil {
		data = map[string]interface{}{}
	}
	if cn.c != nil && cn.processID != 0 {
		data["pid"] = cn.processID
	}

	cn.logger.Log(ctx, lvl, msg, data)
}

func (cn *conn) startTLS(tlsConfig *tls.Config) (err error) {
	err = binary.Write(cn.c, binary.BigEndian, []int32{8, 80877103})
	if err != nil {
		return
	}

	response := make([]byte, 1)
	if _, err = io.ReadFull(cn.c, response); err != nil {
		return
	}

	if response[0] != 'S' {
		return ErrSSLNotSupported
	}

	cn.c = tls.Client(cn.c, tlsConfig)

	return nil
}

func (cn *conn) writeBuf(b byte) *writeBuf {
	cn.scratch[0] = b
	return &writeBuf{
		buf: cn.scratch[:5],
		pos: 1,
	}
}

type values map[string]string

func (cn *conn) isInTransaction() bool {
	return cn.txnStatus == txnStatusIdleInTransaction ||
		cn.txnStatus == txnStatusInFailedTransaction
}

func (cn *conn) setBad() {
	if cn.bad != nil {
		cn.bad.Store(true)
	}
}

func (cn *conn) getBad() bool {
	if cn.bad != nil {
		return cn.bad.Load().(bool)
	}
	return false
}

func (cn *conn) checkIsInTransaction(intxn bool) {
	if cn.isInTransaction() != intxn {
		cn.setBad()
		errorf("unexpected transaction status %v", cn.txnStatus)
	}
}

func (cn *conn) Begin() (_ driver.Tx, err error) {
	return cn.begin("")
}

func (cn *conn) begin(mode string) (_ driver.Tx, err error) {
	if cn.getBad() {
		return nil, driver.ErrBadConn
	}
	defer cn.errRecover(&err)

	cn.checkIsInTransaction(false)
	_, commandTag, err := cn.simpleExec("BEGIN" + mode)
	if err != nil {
		return nil, err
	}
	if commandTag != "BEGIN" {
		cn.setBad()
		return nil, fmt.Errorf("unexpected command tag %s", commandTag)
	}
	if cn.txnStatus != txnStatusIdleInTransaction {
		cn.setBad()
		return nil, fmt.Errorf("unexpected transaction status %v", cn.txnStatus)
	}
	return cn, nil
}

func (cn *conn) closeTxn() {
	if finish := cn.txnFinish; finish != nil {
		finish()
	}
}

func (cn *conn) Commit() (err error) {
	defer cn.closeTxn()
	if cn.getBad() {
		return driver.ErrBadConn
	}
	defer cn.errRecover(&err)

	cn.checkIsInTransaction(true)
	// We don't want the client to think that everything is okay if it tries
	// to commit a failed transaction.  However, no matter what we return,
	// database/sql will release this connection back into the free connection
	// pool so we have to abort the current transaction here.  Note that you
	// would get the same behaviour if you issued a COMMIT in a failed
	// transaction, so it's also the least surprising thing to do here.
	if cn.txnStatus == txnStatusInFailedTransaction {
		if err := cn.rollback(); err != nil {
			return err
		}
		return ErrInFailedTransaction
	}

	_, commandTag, err := cn.simpleExec("COMMIT")
	if err != nil {
		if cn.isInTransaction() {
			cn.setBad()
		}
		return err
	}
	if commandTag != "COMMIT" {
		cn.setBad()
		return fmt.Errorf("unexpected command tag %s", commandTag)
	}
	cn.checkIsInTransaction(false)
	return nil
}

func (cn *conn) Rollback() (err error) {
	defer cn.closeTxn()
	if cn.getBad() {
		return driver.ErrBadConn
	}
	defer cn.errRecover(&err)
	return cn.rollback()
}

func (cn *conn) rollback() (err error) {
	cn.checkIsInTransaction(true)
	_, commandTag, err := cn.simpleExec("ROLLBACK")
	if err != nil {
		if cn.isInTransaction() {
			cn.setBad()
		}
		return err
	}
	if commandTag != "ROLLBACK" {
		return fmt.Errorf("unexpected command tag %s", commandTag)
	}
	cn.checkIsInTransaction(false)
	return nil
}

func (cn *conn) gname() string {
	cn.namei++
	return strconv.FormatInt(int64(cn.namei), 10)
}

func (cn *conn) simpleExec(q string) (res driver.Result, commandTag string, err error) {
	b := cn.writeBuf('Q')
	b.string(q)
	cn.send(b)

	for {
		t, r := cn.recv1()
		switch t {
		case 'C':
			res, commandTag = cn.parseComplete(r.string())
		case 'Z':
			cn.processReadyForQuery(r)
			if res == nil && err == nil {
				err = errUnexpectedReady
			}
			// done
			return
		case 'E':
			err = parseError(r)
		case 'I':
			res = emptyRows
		case 'T', 'D':
			// ignore any results
		default:
			cn.setBad()
			errorf("unknown response for simple query: %q", t)
		}
	}
}

func (cn *conn) simpleQuery(q string) (res *rows, err error) {
	defer cn.errRecover(&err)

	b := cn.writeBuf('Q')
	b.string(q)
	cn.send(b)

	for {
		t, r := cn.recv1()
		switch t {
		case 'C', 'I':
			// We allow queries which don't return any results through Query as
			// well as Exec.  We still have to give database/sql a rows object
			// the user can close, though, to avoid connections from being
			// leaked.  A "rows" with done=true works fine for that purpose.
			if err != nil {
				cn.setBad()
				errorf("unexpected message %q in simple query execution", t)
			}
			if res == nil {
				res = &rows{
					cn: cn,
				}
			}
			// Set the result and tag to the last command complete if there wasn't a
			// query already run. Although queries usually return from here and cede
			// control to Next, a query with zero results does not.
			if t == 'C' {
				res.result, res.tag = cn.parseComplete(r.string())
				if res.colNames != nil {
					return
				}
			}
			res.done = true
		case 'Z':
			cn.processReadyForQuery(r)
			// done
			return
		case 'E':
			res = nil
			err = parseError(r)
		case 'D':
			if res == nil {
				cn.setBad()
				errorf("unexpected DataRow in simple query execution")
			}
			// the query didn't fail; kick off to Next
			cn.saveMessage(t, r)
			return
		case 'T':
			// res might be non-nil here if we received a previous
			// CommandComplete, but that's fine; just overwrite it
			res = &rows{cn: cn}
			res.rowsHeader = parsePortalRowDescribe(r)

			// To work around a bug in QueryRow in Go 1.2 and earlier, wait
			// until the first DataRow has been received.
		default:
			cn.setBad()
			errorf("unknown response for simple query: %q", t)
		}
	}
}

type noRows struct{}

var emptyRows noRows

var _ driver.Result = noRows{}

func (noRows) LastInsertId() (int64, error) {
	return 0, errNoLastInsertID
}

func (noRows) RowsAffected() (int64, error) {
	return 0, errNoRowsAffected
}

// Decides which column formats to use for a prepared statement.  The input is
// an array of type oids, one element per result column.
func decideColumnFormats(colTyps []fieldDesc, forceText bool) (colFmts []format, colFmtData []byte) {
	if len(colTyps) == 0 {
		return nil, colFmtDataAllText
	}

	colFmts = make([]format, len(colTyps))
	if forceText {
		return colFmts, colFmtDataAllText
	}

	allBinary := true
	allText := true
	for i, t := range colTyps {
		switch t.OID {
		// This is the list of types to use binary mode for when receiving them
		// through a prepared statement.  If a type appears in this list, it
		// must also be implemented in binaryDecode in encode.go.
		case oid.T_bytea:
			fallthrough
		case oid.T_int8:
			fallthrough
		case oid.T_int4:
			fallthrough
		case oid.T_int2:
			fallthrough
		case oid.T_uuid:
			colFmts[i] = formatBinary
			allText = false

		default:
			allBinary = false
		}
	}

	if allBinary {
		return colFmts, colFmtDataAllBinary
	} else if allText {
		return colFmts, colFmtDataAllText
	} else {
		colFmtData = make([]byte, 2+len(colFmts)*2)
		binary.BigEndian.PutUint16(colFmtData, uint16(len(colFmts)))
		for i, v := range colFmts {
			binary.BigEndian.PutUint16(colFmtData[2+i*2:], uint16(v))
		}
		return colFmts, colFmtData
	}
}

func (cn *conn) prepareTo(q, stmtName string) *stmt {
	st := &stmt{cn: cn, name: stmtName}

	b := cn.writeBuf('P')
	b.string(st.name)
	b.string(q)
	b.int16(0)

	b.next('D')
	b.byte('S')
	b.string(st.name)

	b.next('S')
	cn.send(b)

	cn.readParseResponse()
	st.paramTyps, st.colNames, st.colTyps = cn.readStatementDescribeResponse()
	st.colFmts, st.colFmtData = decideColumnFormats(st.colTyps, cn.disablePreparedBinaryResult)
	cn.readReadyForQuery()
	return st
}

func (cn *conn) Prepare(q string) (_ driver.Stmt, err error) {
	if cn.getBad() {
		return nil, driver.ErrBadConn
	}
	defer cn.errRecover(&err)

	if len(q) >= 4 && strings.EqualFold(q[:4], "COPY") {
		s, err := cn.prepareCopyIn(q)
		if err == nil {
			cn.inCopy = true
		}
		return s, err
	}
	return cn.prepareTo(q, cn.gname()), nil
}

func (cn *conn) Close() (err error) {
	// Skip cn.bad return here because we always want to close a connection.
	defer cn.errRecover(&err)

	// Ensure that cn.c.Close is always run. Since error handling is done with
	// panics and cn.errRecover, the Close must be in a defer.
	defer func() {
		cerr := cn.c.Close()
		if err == nil {
			err = cerr
		}
	}()

	// Don't go through send(); ListenerConn relies on us not scribbling on the
	// scratch buffer of this connection.
	return cn.sendSimpleMessage('X')
}

// Query Implement the "Queryer" interface
func (cn *conn) Query(query string, args []driver.Value) (driver.Rows, error) {
	return cn.query(query, args)
}

func (cn *conn) query(query string, args []driver.Value) (_ *rows, err error) {
	if cn.getBad() {
		return nil, driver.ErrBadConn
	}
	if cn.inCopy {
		return nil, errCopyInProgress
	}
	defer cn.errRecover(&err)

	// Check to see if we can use the "simpleQuery" interface, which is
	// *much* faster than going through prepare/exec
	if len(args) == 0 {
		return cn.simpleQuery(query)
	}

	if cn.binaryParameters {
		cn.sendBinaryModeQuery(query, args)

		cn.readParseResponse()
		cn.readBindResponse()
		rows := &rows{cn: cn}
		rows.rowsHeader = cn.readPortalDescribeResponse()
		cn.postExecuteWorkaround()
		return rows, nil
	}
	st := cn.prepareTo(query, "")
	st.exec(args)
	return &rows{
		cn:         cn,
		rowsHeader: st.rowsHeader,
	}, nil
}

// Exec Implement the optional "Execer" interface for one-shot queries
func (cn *conn) Exec(query string, args []driver.Value) (res driver.Result, err error) {
	if cn.getBad() {
		return nil, driver.ErrBadConn
	}
	defer cn.errRecover(&err)

	// Check to see if we can use the "simpleExec" interface, which is
	// *much* faster than going through prepare/exec
	if len(args) == 0 {
		// ignore commandTag, our caller doesn't care
		r, _, err := cn.simpleExec(query)
		return r, err
	}

	if cn.binaryParameters {
		cn.sendBinaryModeQuery(query, args)

		cn.readParseResponse()
		cn.readBindResponse()
		cn.readPortalDescribeResponse()
		cn.postExecuteWorkaround()
		res, _, err = cn.readExecuteResponse("Execute")
		return res, err
	}
	// Use the unnamed statement to defer planning until bind
	// time, or else value-based selectivity estimates cannot be
	// used.
	st := cn.prepareTo(query, "")
	r, err := st.Exec(args)
	if err != nil {
		panic(err)
	}
	return r, err
}

type safeRetryError struct {
	Err error
}

func (se *safeRetryError) Error() string {
	return se.Err.Error()
}

func (cn *conn) send(m *writeBuf) {
	n, err := cn.c.Write(m.wrap())
	if err != nil {
		if n == 0 {
			err = &safeRetryError{Err: err}
		}
		panic(err)
	}
}

func (cn *conn) sendStartupPacket(m *writeBuf) error {
	_, err := cn.c.Write((m.wrap())[1:])
	return err
}

// Send a message of type typ to the server on the other end of cn.  The
// message should have no payload.  This method does not use the scratch
// buffer.
func (cn *conn) sendSimpleMessage(typ byte) (err error) {
	_, err = cn.c.Write([]byte{typ, '\x00', '\x00', '\x00', '\x04'})
	return err
}

// saveMessage memorizes a message and its buffer in the conn struct.
// recvMessage will then return these values on the next call to it.  This
// method is useful in cases where you have to see what the next message is
// going to be (e.g. to see whether it's an error or not) but you can't handle
// the message yourself.
func (cn *conn) saveMessage(typ byte, buf *readBuf) {
	if cn.saveMessageType != 0 {
		cn.setBad()
		errorf("unexpected saveMessageType %d", cn.saveMessageType)
	}
	cn.saveMessageType = typ
	cn.saveMessageBuffer = *buf
}

// recvMessage receives any message from the backend, or returns an error if
// a problem occurred while reading the message.
func (cn *conn) recvMessage(r *readBuf) (byte, error) {
	// workaround for a QueryRow bug, see exec
	if cn.saveMessageType != 0 {
		t := cn.saveMessageType
		*r = cn.saveMessageBuffer
		cn.saveMessageType = 0
		cn.saveMessageBuffer = nil
		return t, nil
	}

	x := cn.scratch[:5]
	_, err := io.ReadFull(cn.buf, x)
	if err != nil {
		return 0, err
	}

	// read the type and length of the message that follows
	t := x[0]
	n := int(binary.BigEndian.Uint32(x[1:])) - 4
	var y []byte
	if n <= len(cn.scratch) {
		y = cn.scratch[:n]
	} else {
		y = make([]byte, n)
	}
	_, err = io.ReadFull(cn.buf, y)
	if err != nil {
		return 0, err
	}
	*r = y
	return t, nil
}

// recv receives a message from the backend, but if an error happened while
// reading the message or the received message was an ErrorResponse, it panics.
// NoticeResponses are ignored.  This function should generally be used only
// during the startup sequence.
func (cn *conn) recv() (t byte, r *readBuf) {
	for {
		var err error
		r = &readBuf{}
		t, err = cn.recvMessage(r)
		if err != nil {
			panic(err)
		}
		switch t {
		case 'E':
			panic(parseError(r))
		case 'N':
			if n := cn.noticeHandler; n != nil {
				n(parseError(r))
			}
		case 'A':
			if n := cn.notificationHandler; n != nil {
				n(recvNotification(r))
			}
		default:
			return
		}
	}
}

// recv1Buf is exactly equivalent to recv1, except it uses a buffer supplied by
// the caller to avoid an allocation.
func (cn *conn) recv1Buf(r *readBuf) byte {
	for {
		t, err := cn.recvMessage(r)
		if err != nil {
			panic(err)
		}

		switch t {
		case 'A':
			if n := cn.notificationHandler; n != nil {
				n(recvNotification(r))
			}
		case 'N':
			if n := cn.noticeHandler; n != nil {
				n(parseError(r))
			}
		case 'S':
			cn.processParameterStatus(r)
		default:
			return t
		}
	}
}

// recv1 receives a message from the backend, panicking if an error occurs
// while attempting to read it.  All asynchronous messages are ignored, with
// the exception of ErrorResponse.
func (cn *conn) recv1() (t byte, r *readBuf) {
	r = &readBuf{}
	t = cn.recv1Buf(r)
	return t, r
}

func (cn *conn) startup() {
	w := cn.writeBuf(0)
	//
	// w.int32(196608)
	//
	// PROTOCOL_VERSION_350
	// PROTOCOL_VERSION_351 196659
	w.int32(196659)
	// Send the backend the name of the database we want to connect to, and the
	// user we want to connect as.  Additionally, we send over any run-time
	// parameters potentially included in the connection string.  If the server
	// doesn't recognize any of them, it will reply with an error.

	for k, v := range cn.config.RuntimeParams {
		w.string(k)
		w.string(v)
	}
	if cn.config.Database != "" {
		w.string("database")
		w.string(cn.config.Database)
	}
	w.string("user")
	w.string(cn.config.User)

	w.string("")
	if err := cn.sendStartupPacket(w); err != nil {
		panic(err)
	}

	for {
		t, r := cn.recv()
		switch t {
		case 'K':
			cn.processBackendKeyData(r)
		case 'S':
			cn.processParameterStatus(r)
		case 'R':
			cn.auth(r)
		case 'Z':
			cn.processReadyForQuery(r)
			if found, err := cn.ValidateConnect(); err != nil {
				cn.c.Close()
				panic(err)
			} else if found {
				return
			}
			errorf("ValidateConnect failed")
		default:
			errorf("unknown response for startup: %q", t)
		}
	}
}

func (cn *conn) auth(r *readBuf) {
	switch code := r.int32(); code {
	case AuthReqOk:
		// OK
	case AuthReqPassword:
		w := cn.writeBuf('p')
		w.string(cn.config.Password)
		cn.send(w)

		t, r := cn.recv()
		if t != 'R' {
			errorf("unexpected password response: %q", t)
		}

		if r.int32() != 0 {
			errorf("unexpected authentication response: %q", t)
		}
	case AuthReqMd5:
		s := string(r.next(4))
		w := cn.writeBuf('p')
		w.string("md5" + md5s(md5s(cn.config.Password+cn.config.User)+s))
		cn.send(w)

		t, r := cn.recv()
		if t != 'R' {
			errorf("unexpected password response: %q", t)
		}

		if r.int32() != 0 {
			errorf("unexpected authentication response: %q", t)
		}
	case AuthReqGss: // GSSAPI, startup
		if newGss == nil {
			errorf("kerberos error: no GSSAPI provider registered (import gitee.com/opengauss/openGauss-connector-go-pq/auth/kerberos if you need Kerberos support)")
		}
		cli, err := newGss()
		if err != nil {
			errorf("kerberos error: %s", err.Error())
		}

		var token []byte

		if spn, ok := cn.config.GssAPIParams["krbspn"]; ok {
			// Use the supplied SPN if provided..
			token, err = cli.GetInitTokenFromSpn(spn)
		} else {
			// Allow the kerberos service name to be overridden
			service := "postgres"
			if val, ok := cn.config.GssAPIParams["krbsrvname"]; ok {
				service = val
			}

			token, err = cli.GetInitToken(cn.fallbackConfig.Host, service)
		}

		if err != nil {
			errorf("failed to get Kerberos ticket: %q", err)
		}

		w := cn.writeBuf('p')
		w.bytes(token)
		cn.send(w)

		// Store for GSSAPI continue message
		cn.gss = cli

	case AuthReqGssContinue: // GSSAPI continue

		if cn.gss == nil {
			errorf("GSSAPI protocol error")
		}

		b := []byte(*r)

		done, tokOut, err := cn.gss.Continue(b)
		if err == nil && !done {
			w := cn.writeBuf('p')
			w.bytes(tokOut)
			cn.send(w)
		}

	case AuthReqSha256:

		// 这里在openGauss为sha256加密办法，主要代码流程来自jdbc相关实现
		passwordStoredMethod := r.int32()
		digest := ""
		if len(cn.config.Password) == 0 {
			errorf("The server requested password-based authentication, but no password was provided.")
		}
		if passwordStoredMethod == PlainPassword || passwordStoredMethod == Sha256Password {
			random64code := string(r.next(64))
			token := string(r.next(8))
			serverIteration := r.int32()
			result := RFC5802Algorithm(cn.config.Password, random64code, token, "", serverIteration, "sha256")
			if len(result) == 0 {
				errorf("Invalid username/password,login denied.")
			}
			w := cn.writeBuf('p')
			w.buf = []byte("p")
			w.pos = 1
			w.int32(4 + len(result) + 1)
			w.bytes(result)
			w.byte(0)
			cn.send(w)

			t, r := cn.recv()

			if t != 'R' {
				errorf("unexpected password response: %q", t)
			}

			if r.int32() != 0 {
				errorf("unexpected authentication response: %q", t)
			}
			// return
		} else if passwordStoredMethod == Md5Password {
			s := string(r.next(4))
			digest = "md5" + md5s(md5s(cn.config.Password+cn.config.User)+s)
			w := cn.writeBuf('p')
			w.int16(4 + len(digest) + 1)
			w.string(digest)
			w.byte(0)
			cn.send(w)
			t, r := cn.recv()
			if t != 'R' {
				errorf("unexpected password response: %q", t)
			}

			if r.int32() != 0 {
				errorf("unexpected authentication response: %q", t)
			}
		} else {
			errorf("The  password-stored method is not supported ,must be plain, md5 or sha256.")
		}

	// AUTH_REQ_MD5_SHA256
	case AuthReqMd5Sha256:
		random64code := string(r.next(64))
		md5Salt := r.next(4)
		result := Md5Sha256encode(cn.config.Password, random64code, md5Salt)
		digest := []byte("md5")
		digest = append(digest, result...)
		w := cn.writeBuf('p')
		w.int32(4 + len(digest) + 1)
		w.bytes(digest)
		w.byte(0)
		cn.send(w)

		t, r := cn.recv()

		if t != 'R' {
			errorf("unexpected password response: %q", t)
		}

		if r.int32() != 0 {
			errorf("unexpected authentication response: %q", t)
		}
	case AuthReqSm3: // sm3
		passwordStoredMethod := r.int32()
		if passwordStoredMethod == Sm3Password {
			random64code := string(r.next(64))
			token := string(r.next(8))
			serverIteration := r.int32()

			result := RFC5802Algorithm(cn.config.Password, random64code, token, "", serverIteration, "sm3")
			if len(result) == 0 {
				errorf("Invalid username/password,login denied.")
			}

			w := cn.writeBuf('p')
			w.buf = []byte("p")
			w.pos = 1
			w.int32(4 + len(result) + 1)
			w.bytes(result)
			w.byte(0)
			cn.send(w)

			t, r := cn.recv()

			if t != 'R' {
				errorf("unexpected password response: %q", t)
			}

			if r.int32() != 0 {
				errorf("unexpected authentication response: %q", t)
			}
		} else {
			errorf("The password-stored method is not supported ,must be sm3.")
		}
	default:
		errorf("unknown authentication response: %d", code)
	}
}

func (cn *conn) ValidateConnect() (bool, error) {
	if cn.config.targetSessionAttrs == "" {
		return true, nil
	}
	sqlText := "show transaction_read_only"

	cn.log(context.Background(), LogLevelDebug, "Check server is transaction_read_only ?", map[string]interface{}{"sql": sqlText,
		"host": cn.config.Host, "port": cn.config.Port, "target_session_attrs": cn.config.targetSessionAttrs})
	inReRows, err := cn.query(sqlText, nil)
	if err != nil {
		cn.log(context.Background(), LogLevelDebug, "err:"+err.Error(), map[string]interface{}{})
		return false, err
	}
	defer inReRows.Close()
	var dbTranReadOnly string
	lastCols := []driver.Value{&dbTranReadOnly}
	err = inReRows.Next(lastCols)
	if err != nil {
		cn.log(context.Background(), LogLevelDebug, "err:"+err.Error(), map[string]interface{}{})
		return false, err
	}
	readOnly := lastCols[0].(string)
	cn.log(context.Background(), LogLevelDebug, "Check server is readOnly ?", map[string]interface{}{"readOnly": readOnly,
		"host": cn.config.Host, "port": cn.config.Port})

	if strings.EqualFold(cn.config.targetSessionAttrs, targetSessionAttrsReadWrite) &&
		strings.EqualFold(readOnly, "off") {
		return true, nil
	} else if strings.EqualFold(cn.config.targetSessionAttrs, targetSessionAttrsReadOnly) &&
		strings.EqualFold(readOnly, "on") {
		return true, nil
	} else {
		return false, nil
	}

}

type format int

const formatText format = 0
const formatBinary format = 1

// One result-column format code with the value 1 (i.e. all binary).
var colFmtDataAllBinary = []byte{0, 1, 0, 1}

// No result-column format codes (i.e. all text).
var colFmtDataAllText = []byte{0, 0}

type stmt struct {
	cn   *conn
	name string
	rowsHeader
	colFmtData []byte
	paramTyps  []oid.Oid
	closed     bool
}

func (st *stmt) Close() (err error) {
	if st.closed {
		return nil
	}
	if st.cn.getBad() {
		return driver.ErrBadConn
	}
	defer st.cn.errRecover(&err)

	w := st.cn.writeBuf('C')
	w.byte('S')
	w.string(st.name)
	st.cn.send(w)

	st.cn.send(st.cn.writeBuf('S'))

	t, _ := st.cn.recv1()
	if t != '3' {
		st.cn.setBad()
		errorf("unexpected close response: %q", t)
	}
	st.closed = true

	t, r := st.cn.recv1()
	if t != 'Z' {
		st.cn.setBad()
		errorf("expected ready for query, but got: %q", t)
	}
	st.cn.processReadyForQuery(r)

	return nil
}

func (st *stmt) Query(v []driver.Value) (r driver.Rows, err error) {
	if st.cn.getBad() {
		return nil, driver.ErrBadConn
	}
	defer st.cn.errRecover(&err)

	st.exec(v)
	return &rows{
		cn:         st.cn,
		rowsHeader: st.rowsHeader,
	}, nil
}

func (st *stmt) Exec(v []driver.Value) (res driver.Result, err error) {
	if st.cn.getBad() {
		return nil, driver.ErrBadConn
	}
	defer st.cn.errRecover(&err)

	st.exec(v)
	res, _, err = st.cn.readExecuteResponse("simple query")
	return res, err
}

func (st *stmt) exec(v []driver.Value) {
	if len(v) >= 65536 {
		errorf("got %d parameters but PostgreSQL only supports 65535 parameters", len(v))
	}
	if len(v) != len(st.paramTyps) {
		errorf("got %d parameters but the statement requires %d", len(v), len(st.paramTyps))
	}

	cn := st.cn
	w := cn.writeBuf('B')
	w.byte(0) // unnamed portal
	w.string(st.name)

	if cn.binaryParameters {
		cn.sendBinaryParameters(w, v)
	} else {
		w.int16(0)
		w.int16(len(v))
		for i, x := range v {
			if x == nil {
				w.int32(-1)
			} else {
				b := encode(&cn.parameterStatus, x, st.paramTyps[i])
				w.int32(len(b))
				w.bytes(b)
			}
		}
	}
	w.bytes(st.colFmtData)

	w.next('E')
	w.byte(0)
	w.int32(0)

	w.next('S')
	cn.send(w)

	cn.readBindResponse()
	cn.postExecuteWorkaround()

}

func (st *stmt) NumInput() int {
	return len(st.paramTyps)
}

// parseComplete parses the "command tag" from a CommandComplete message, and
// returns the number of rows affected (if applicable) and a string
// identifying only the command that was executed, e.g. "ALTER TABLE".  If the
// command tag could not be parsed, parseComplete panics.
func (cn *conn) parseComplete(commandTag string) (driver.Result, string) {
	commandsWithAffectedRows := []string{
		"SELECT ",
		// INSERT is handled below
		"UPDATE ",
		"DELETE ",
		"FETCH ",
		"MOVE ",
		"COPY ",
	}

	var affectedRows *string
	for _, tag := range commandsWithAffectedRows {
		if strings.HasPrefix(commandTag, tag) {
			t := commandTag[len(tag):]
			affectedRows = &t
			commandTag = tag[:len(tag)-1]
			break
		}
	}
	// INSERT also includes the oid of the inserted row in its command tag.
	// Oids in user tables are deprecated, and the oid is only returned when
	// exactly one row is inserted, so it's unlikely to be of value to any
	// real-world application and we can ignore it.
	if affectedRows == nil && strings.HasPrefix(commandTag, "INSERT ") {
		parts := strings.Split(commandTag, " ")
		if len(parts) != 3 {
			cn.setBad()
			errorf("unexpected INSERT command tag %s", commandTag)
		}
		affectedRows = &parts[len(parts)-1]
		commandTag = "INSERT"
	}
	// There should be no affected rows attached to the tag, just return it
	if affectedRows == nil {
		return driver.RowsAffected(0), commandTag
	}
	n, err := strconv.ParseInt(*affectedRows, 10, 64)
	if err != nil {
		cn.setBad()
		errorf("could not parse commandTag: %s", err)
	}
	return driver.RowsAffected(n), commandTag
}

// QuoteIdentifier quotes an "identifier" (e.g. a table or a column name) to be
// used as part of an SQL statement.  For example:
//
//    tblname := "my_table"
//    data := "my_data"
//    quoted := pq.QuoteIdentifier(tblname)
//    err := db.Exec(fmt.Sprintf("INSERT INTO %s VALUES ($1)", quoted), data)
//
// Any double quotes in name will be escaped.  The quoted identifier will be
// case sensitive when used in a query.  If the input string contains a zero
// byte, the result will be truncated immediately before it.
func QuoteIdentifier(name string) string {
	end := strings.IndexRune(name, 0)
	if end > -1 {
		name = name[:end]
	}
	return `"` + strings.Replace(name, `"`, `""`, -1) + `"`
}

// QuoteLiteral quotes a 'literal' (e.g. a parameter, often used to pass literal
// to DDL and other statements that do not accept parameters) to be used as part
// of an SQL statement.  For example:
//
//    exp_date := pq.QuoteLiteral("2023-01-05 15:00:00Z")
//    err := db.Exec(fmt.Sprintf("CREATE ROLE my_user VALID UNTIL %s", exp_date))
//
// Any single quotes in name will be escaped. Any backslashes (i.e. "\") will be
// replaced by two backslashes (i.e. "\\") and the C-style escape identifier
// that PostgreSQL provides ('E') will be prepended to the string.
func QuoteLiteral(literal string) string {
	// This follows the PostgreSQL internal algorithm for handling quoted literals
	// from libpq, which can be found in the "PQEscapeStringInternal" function,
	// which is found in the libpq/fe-exec.c source file:
	// https://git.postgresql.org/gitweb/?p=postgresql.git;a=blob;f=src/interfaces/libpq/fe-exec.c
	//
	// substitute any single-quotes (') with two single-quotes ('')
	literal = strings.Replace(literal, `'`, `''`, -1)
	// determine if the string has any backslashes (\) in it.
	// if it does, replace any backslashes (\) with two backslashes (\\)
	// then, we need to wrap the entire string with a PostgreSQL
	// C-style escape. Per how "PQEscapeStringInternal" handles this case, we
	// also add a space before the "E"
	if strings.Contains(literal, `\`) {
		literal = strings.Replace(literal, `\`, `\\`, -1)
		literal = ` E'` + literal + `'`
	} else {
		// otherwise, we can just wrap the literal with a pair of single quotes
		literal = `'` + literal + `'`
	}
	return literal
}

func md5s(s string) string {
	h := md5.New()
	h.Write([]byte(s))
	return fmt.Sprintf("%x", h.Sum(nil))
}

func (cn *conn) sendBinaryParameters(b *writeBuf, args []driver.Value) {
	// Do one pass over the parameters to see if we're going to send any of
	// them over in binary.  If we are, create a paramFormats array at the
	// same time.
	var paramFormats []int
	for i, x := range args {
		_, ok := x.([]byte)
		if ok {
			if paramFormats == nil {
				paramFormats = make([]int, len(args))
			}
			paramFormats[i] = 1
		}
	}
	if paramFormats == nil {
		b.int16(0)
	} else {
		b.int16(len(paramFormats))
		for _, x := range paramFormats {
			b.int16(x)
		}
	}

	b.int16(len(args))
	for _, x := range args {
		if x == nil {
			b.int32(-1)
		} else {
			datum := binaryEncode(&cn.parameterStatus, x)
			b.int32(len(datum))
			b.bytes(datum)
		}
	}
}

func (cn *conn) sendBinaryModeQuery(query string, args []driver.Value) {
	if len(args) >= 65536 {
		errorf("got %d parameters but PostgreSQL only supports 65535 parameters", len(args))
	}

	b := cn.writeBuf('P')
	b.byte(0) // unnamed statement
	b.string(query)
	b.int16(0)

	b.next('B')
	b.int16(0) // unnamed portal and statement
	cn.sendBinaryParameters(b, args)
	b.bytes(colFmtDataAllText)

	b.next('D')
	b.byte('P')
	b.byte(0) // unnamed portal

	b.next('E')
	b.byte(0)
	b.int32(0)

	b.next('S')
	cn.send(b)
}

func (cn *conn) processParameterStatus(r *readBuf) {
	var err error

	param := r.string()
	switch param {
	case "server_version":
		var major1 int
		var major2 int
		_, err = fmt.Sscanf(r.string(), "%d.%d", &major1, &major2)
		if err == nil {
			cn.parameterStatus.serverVersion = major1*10000 + major2*100
		}

	case "TimeZone":
		cn.parameterStatus.currentLocation, err = time.LoadLocation(r.string())
		if err != nil {
			cn.parameterStatus.currentLocation = nil
		}

	default:
		// ignore
	}
}

func (cn *conn) processReadyForQuery(r *readBuf) {
	cn.txnStatus = transactionStatus(r.byte())
}

func (cn *conn) readReadyForQuery() {
	t, r := cn.recv1()
	switch t {
	case 'Z':
		cn.processReadyForQuery(r)
		return
	default:
		cn.setBad()
		errorf("unexpected message %q; expected ReadyForQuery", t)
	}
}

func (cn *conn) processBackendKeyData(r *readBuf) {
	cn.processID = r.int32()
	cn.secretKey = r.int32()
}

func (cn *conn) readParseResponse() {
	t, r := cn.recv1()
	switch t {
	case '1':
		return
	case 'E':
		err := parseError(r)
		cn.readReadyForQuery()
		panic(err)
	default:
		cn.setBad()
		errorf("unexpected Parse response %q", t)
	}
}

func (cn *conn) readStatementDescribeResponse() (paramTyps []oid.Oid, colNames []string, colTyps []fieldDesc) {
	for {
		t, r := cn.recv1()
		switch t {
		case 't':
			nparams := r.int16()
			paramTyps = make([]oid.Oid, nparams)
			for i := range paramTyps {
				paramTyps[i] = r.oid()
			}
		case 'n':
			return paramTyps, nil, nil
		case 'T':
			colNames, colTyps = parseStatementRowDescribe(r)
			return paramTyps, colNames, colTyps
		case 'E':
			err := parseError(r)
			cn.readReadyForQuery()
			panic(err)
		default:
			cn.setBad()
			errorf("unexpected Describe statement response %q", t)
		}
	}
}

func (cn *conn) readPortalDescribeResponse() rowsHeader {
	t, r := cn.recv1()
	switch t {
	case 'T':
		return parsePortalRowDescribe(r)
	case 'n':
		return rowsHeader{}
	case 'E':
		err := parseError(r)
		cn.readReadyForQuery()
		panic(err)
	default:
		cn.setBad()
		errorf("unexpected Describe response %q", t)
	}
	panic("not reached")
}

func (cn *conn) readBindResponse() {
	t, r := cn.recv1()
	switch t {
	case '2':
		return
	case 'E':
		err := parseError(r)
		cn.readReadyForQuery()
		panic(err)
	default:
		cn.setBad()
		errorf("unexpected Bind response %q", t)
	}
}

func (cn *conn) postExecuteWorkaround() {
	// Work around a bug in sql.DB.QueryRow: in Go 1.2 and earlier it ignores
	// any errors from rows.Next, which masks errors that happened during the
	// execution of the query.  To avoid the problem in common cases, we wait
	// here for one more message from the database.  If it's not an error the
	// query will likely succeed (or perhaps has already, if it's a
	// CommandComplete), so we push the message into the conn struct; recv1
	// will return it as the next message for rows.Next or rows.Close.
	// However, if it's an error, we wait until ReadyForQuery and then return
	// the error to our caller.
	for {
		t, r := cn.recv1()
		switch t {
		case 'E':
			err := parseError(r)
			cn.readReadyForQuery()
			panic(err)
		case 'C', 'D', 'I':
			// the query didn't fail, but we can't process this message
			cn.saveMessage(t, r)
			return
		default:
			cn.setBad()
			errorf("unexpected message during extended query execution: %q", t)
		}
	}
}

// Only for Exec(), since we ignore the returned data
func (cn *conn) readExecuteResponse(protocolState string) (res driver.Result, commandTag string, err error) {
	for {
		t, r := cn.recv1()
		switch t {
		case 'C':
			if err != nil {
				cn.setBad()
				errorf("unexpected CommandComplete after error %s", err)
			}
			res, commandTag = cn.parseComplete(r.string())
		case 'Z':
			cn.processReadyForQuery(r)
			if res == nil && err == nil {
				err = errUnexpectedReady
			}
			return res, commandTag, err
		case 'E':
			err = parseError(r)
		case 'T', 'D', 'I':
			if err != nil {
				cn.setBad()
				errorf("unexpected %q after error %s", t, err)
			}
			if t == 'I' {
				res = emptyRows
			}
			// ignore any results
		default:
			cn.setBad()
			errorf("unknown %s response: %q", protocolState, t)
		}
	}
}

func parseStatementRowDescribe(r *readBuf) (colNames []string, colTyps []fieldDesc) {
	n := r.int16()
	colNames = make([]string, n)
	colTyps = make([]fieldDesc, n)
	for i := range colNames {
		colNames[i] = r.string()
		r.next(6)
		colTyps[i].OID = r.oid()
		colTyps[i].Len = r.int16()
		colTyps[i].Mod = r.int32()
		// format code not known when describing a statement; always 0
		r.next(2)
	}
	return
}

func parsePortalRowDescribe(r *readBuf) rowsHeader {
	n := r.int16()
	colNames := make([]string, n)
	colFmts := make([]format, n)
	colTyps := make([]fieldDesc, n)
	for i := range colNames {
		colNames[i] = r.string()
		r.next(6)
		colTyps[i].OID = r.oid()
		colTyps[i].Len = r.int16()
		colTyps[i].Mod = r.int32()
		colFmts[i] = format(r.int16())
	}
	return rowsHeader{
		colNames: colNames,
		colFmts:  colFmts,
		colTyps:  colTyps,
	}
}

// isUTF8 returns whether name is a fuzzy variation of the string "UTF-8".
func isUTF8(name string) bool {
	// Recognize all sorts of silly things as "UTF-8", like Postgres does
	s := strings.Map(alnumLowerASCII, name)
	return s == "utf8" || s == "unicode"
}

func alnumLowerASCII(ch rune) rune {
	if 'A' <= ch && ch <= 'Z' {
		return ch + ('a' - 'A')
	}
	if 'a' <= ch && ch <= 'z' || '0' <= ch && ch <= '9' {
		return ch
	}
	return -1 // discard
}
