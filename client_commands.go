package memcached

import (
	"bytes"
	"context"

	"github.com/pkg/errors"
)

type basicTextProtocolCommander interface {
	/**
	Storage commands: set, add, replace, append, prepend, cas
	*/

	// Set is used to store the given key-value pair.
	//
	// flags is an arbitrary 32-bit unsigned integer (written out in decimal) that
	// the server stores along with the data and sends back when the item is retrieved.
	//
	// expiry is the TTL of the key in seconds.
	Set(ctx context.Context, key string, value []byte, flags, expiry uint32) error
	// Add is used to store the given key-value pair if the key does not exist.
	//
	// flags is an arbitrary 32-bit unsigned integer (written out in decimal) that
	// the server stores along with the data and sends back when the item is retrieved.
	//
	// expiry is the TTL of the key in seconds.
	Add(ctx context.Context, key string, value []byte, flags, expiry uint32) error
	// Replace is used to update the value of an existing item.
	//
	// flags is an arbitrary 32-bit unsigned integer (written out in decimal) that
	// the server stores along with the data and sends back when the item is retrieved.
	//
	// expiry is the TTL of the key in seconds.
	Replace(ctx context.Context, key string, value []byte, flags, expiry uint32) error
	// Append is used to append the value to an existing item.
	//
	// flags is an arbitrary 32-bit unsigned integer (written out in decimal) that
	// the server stores along with the data and sends back when the item is retrieved.
	//
	// expiry is the TTL of the key in seconds.
	Append(ctx context.Context, key string, value []byte, flags, expiry uint32) error
	// Prepend is used to prepend the value to an existing item.
	//
	// flags is an arbitrary 32-bit unsigned integer (written out in decimal) that
	// the server stores along with the data and sends back when the item is retrieved.
	//
	// expiry is the TTL of the key in seconds.
	Prepend(ctx context.Context, key string, value []byte, flags, expiry uint32) error

	// Cas is used to update the value of an existing item and also check-and-set operation.
	//
	// flags is an arbitrary 32-bit unsigned integer (written out in decimal) that
	// the server stores along with the data and sends back when the item is retrieved.
	//
	// expiry is the TTL of the key in seconds.
	Cas(ctx context.Context, key string, value []byte, flags, expiry uint32, cas uint64) error

	/**
	Retrieval commands: get and gets
	*/

	// Get gets the value of the given key.
	//
	// This command would not return the <cas unique> value, using `Gets` instead.
	Get(ctx context.Context, key string) (*Item, error)
	// Gets the values of the given keys.
	//
	// BUT you must know that the cluster mode of memcached DOES NOT support this command,
	// since keys are possible stored in different memcached instances.
	// Be careful when using this command unless you are sure that
	// all keys are stored in the same memcached instance.
	//
	// Gets will return the <cas unique> value which is used to check-and-set operation.
	Gets(ctx context.Context, keys ...string) ([]*Item, error)
	// GetAndTouch is used to get the value of the given key and update the expiration time of the key.
	GetAndTouch(ctx context.Context, expiry uint32, key string) (*Item, error)
	// GetAndTouches is used to get the values of the given keys and update the expiration time of the keys.
	//
	// BUT you must know that the cluster mode of memcached DOES NOT support this command,
	// since keys are possible stored in different memcached instances.
	// Be careful when using this command unless you are sure that
	// all keys are stored in the same memcached instance.
	GetAndTouches(ctx context.Context, expiry uint32, keys ...string) ([]*Item, error)
	/**
	Other commands: delete
	*/

	// Delete is used to delete the given key.
	Delete(ctx context.Context, key string) error
	// Incr is used to increment the value of the given key.
	// If noReply mode enabled, it will return 0.
	Incr(ctx context.Context, key string, delta uint64) (uint64, error)
	// Decr is used to decrement the value of the given key.
	// If noReply mode enabled, it will return 0.
	Decr(ctx context.Context, key string, delta uint64) (uint64, error)
	// Touch is used to update the expiration time of an existing item
	// without fetching it.
	//
	// expiry is the TTL of the key in seconds.
	Touch(ctx context.Context, key string, expiry uint32) error

	// Version is used to get the version of the memcached server.
	// TODO(@yeqown): auto-detect the version of the memcached server.
	// 	to support limit some commands that are not supported by the server.
	//  e.g. flags are uint16 only before 1.2.1
	// 	e.g. SASL authentication is supported after 1.4.3
	Version(ctx context.Context) (string, error)

	// FlushAll is used to flush all data in the memcached server.
	FlushAll(ctx context.Context) error
}

type metaTextProtocolCommander interface {
	// MetaSet is used to store the given key-value pair with metadata.
	// All available options start with MetaSetFlagXXX, such as MetaSetFlagBinaryKey
	// and MetaSetFlagReturnCAS.
	MetaSet(ctx context.Context, key, value []byte, options ...MetaSetOption) (*MetaItem, error)
	// MetaGet is used to get the value of the given key with metadata.
	// All available options start with MetaGetFlagXXX, such as MetaGetFlagReturnCAS
	// and MetaGetFlagReturnClientFlags.
	MetaGet(ctx context.Context, key []byte, options ...MetaGetOption) (*MetaItem, error)
	// MetaDelete is used to delete the given key with metadata.
	// All available options start with MetaDeleteFlagXXX, such as MetaDeleteFlagRemoveValueOnly
	// and MetaDeleteFlagUpdateTTL.
	MetaDelete(ctx context.Context, key []byte, options ...MetaDeleteOption) (*MetaItem, error)
	// MetaArithmetic is used to increment or decrement the value of the given key with metadata.
	// All available options start with MetaArithmeticFlagXXX, such as MetaArithmeticFlagReturnCAS
	// and MetaArithmeticFlagReturnClientFlags.
	MetaArithmetic(ctx context.Context, key []byte, delta uint64, options ...MetaArithmeticOption) (*MetaItem, error)
	// MetaDebug is used to get the debug information of the given key with metadata.
	// All available options start with MetaDebugFlagXXX, such as MetaDebugFlagBinaryKey
	MetaDebug(ctx context.Context, key []byte, options ...MetaDebugOption) (*MetaItemDebug, error)
	// MetaNoOp is used to do nothing but return OK.
	MetaNoOp(ctx context.Context) error
}

type statisticsTextProtocolCommander interface {
	// TODO: add more statistics commands

	Stats(ctx context.Context, args ...string) (map[string]string, error)
}

/**
 * Storage commands: set, add, replace, append, prepend, cas
 */

func (c *client) storageCommand(ctx context.Context, command, key string, value []byte, flags, expiry uint32) error {
	if err := validateKeyAndValue([]byte(key), nil); err != nil {
		return err
	}

	req, resp := buildStorageCommand(command, key, value, flags, expiry, c.options.noReply)
	defer releaseReqAndResp(req, resp)

	if err := c.dispatchRequest(ctx, req, resp); err != nil {
		return errors.Wrap(err, "request failed")
	}

	// expect STORED\r\n
	if err := resp.expect(_StoredCRLFBytes); err != nil {
		return errors.Wrap(ErrMalformedResponse, err.Error())
	}

	return nil
}

func (c *client) Set(ctx context.Context, key string, value []byte, flags, expiry uint32) error {
	return c.storageCommand(ctx, "set", key, value, flags, expiry)
}

func (c *client) Add(ctx context.Context, key string, value []byte, flags, expiry uint32) error {
	return c.storageCommand(ctx, "add", key, value, flags, expiry)
}

func (c *client) Replace(ctx context.Context, key string, value []byte, flags, expiry uint32) error {
	return c.storageCommand(ctx, "replace", key, value, flags, expiry)
}

func (c *client) Append(ctx context.Context, key string, value []byte, flags, expiry uint32) error {
	return c.storageCommand(ctx, "append", key, value, flags, expiry)
}

func (c *client) Prepend(ctx context.Context, key string, value []byte, flags, expiry uint32) error {
	return c.storageCommand(ctx, "prepend", key, value, flags, expiry)
}

func (c *client) Cas(ctx context.Context, key string, value []byte, flags, expiry uint32, cas uint64) error {
	if err := validateKeyAndValue([]byte(key), value); err != nil {
		return err
	}

	req, resp := buildCasCommand(key, []byte(value), flags, expiry, cas, c.options.noReply)
	defer releaseReqAndResp(req, resp)

	if err := c.dispatchRequest(ctx, req, resp); err != nil {
		return errors.Wrap(err, "request failed")
	}

	// expect STORED\r\n
	if err := resp.expect(_StoredCRLFBytes); err != nil {
		return errors.Wrap(ErrMalformedResponse, err.Error())
	}

	return nil
}

/**
 * Retrieval commands: get, gets, gat, gats
 */

func (c *client) Get(ctx context.Context, key string) (*Item, error) {
	if err := validateKeyAndValue([]byte(key), nil); err != nil {
		return nil, err
	}

	req, resp := buildGetsCommand("get", key)
	defer releaseReqAndResp(req, resp)

	if err := c.dispatchRequest(ctx, req, resp); err != nil {
		return nil, errors.Wrap(err, "request failed")
	}

	// parse response
	items, err := parseValueItems(resp.rawLines, false, false)
	if err != nil {
		return nil, errors.Wrap(err, "parse values failed")
	}
	if len(items) == 0 {
		return nil, errors.Wrap(ErrNotFound, "no items found")
	}

	return items[0], nil
}

func (c *client) Gets(ctx context.Context, keys ...string) ([]*Item, error) {
	if len(keys) == 0 {
		return []*Item{}, nil
	}

	req, resp := buildGetsCommand("gets", keys...)
	defer releaseReqAndResp(req, resp)

	if err := c.dispatchRequest(ctx, req, resp); err != nil {
		return nil, errors.Wrap(err, "request failed")
	}

	// parse response
	items, err := parseValueItems(resp.rawLines, false, true)
	if err != nil {
		return nil, errors.Wrap(ErrMalformedResponse, "parse values failed")
	}
	if len(items) == 0 {
		return nil, errors.Wrap(ErrNotFound, "no items found")
	}

	return items, nil
}

func (c *client) GetAndTouch(ctx context.Context, expiry uint32, key string) (*Item, error) {
	if err := validateKeyAndValue([]byte(key), nil); err != nil {
		return nil, err
	}

	req, resp := buildGetAndTouchesCommand("gat", expiry, key)
	defer releaseReqAndResp(req, resp)

	if err := c.dispatchRequest(ctx, req, resp); err != nil {
		return nil, errors.Wrap(err, "request failed")
	}

	// parse response
	items, err := parseValueItems(resp.rawLines, false, false)
	if err != nil {
		return nil, errors.Wrap(ErrMalformedResponse, "parse values failed")
	}

	if len(items) == 0 {
		return nil, errors.Wrap(ErrNotFound, "no items found")
	}

	return items[0], nil
}

func (c *client) GetAndTouches(ctx context.Context, expiry uint32, keys ...string) ([]*Item, error) {
	if len(keys) == 0 {
		return []*Item{}, nil
	}

	req, resp := buildGetAndTouchesCommand("gats", expiry, keys...)
	defer releaseReqAndResp(req, resp)

	if err := c.dispatchRequest(ctx, req, resp); err != nil {
		return nil, errors.Wrap(err, "request failed")
	}

	// parse response
	items, err := parseValueItems(resp.rawLines, false, true)
	if err != nil {
		return nil, errors.Wrap(ErrMalformedResponse, "parse values failed")
	}

	if len(items) == 0 {
		return nil, errors.Wrap(ErrNotFound, "no items found")
	}

	return items, nil
}

/**
 * Other commands: delete, incr, decr, touch, version, flush_all
 */

func (c *client) Delete(ctx context.Context, key string) error {
	if err := validateKeyAndValue([]byte(key), nil); err != nil {
		return err
	}

	req, resp := buildDeleteCommand(key, c.options.noReply)
	defer releaseReqAndResp(req, resp)

	if err := c.dispatchRequest(ctx, req, resp); err != nil {
		return errors.Wrap(err, "request failed")
	}

	// expect DELETED\r\n
	if err := resp.expect(_DeletedCRLFBytes); err != nil {
		return errors.Wrap(ErrMalformedResponse, err.Error())
	}

	return nil
}

func (c *client) Incr(ctx context.Context, key string, delta uint64) (uint64, error) {
	if err := validateKeyAndValue([]byte(key), nil); err != nil {
		return 0, err
	}

	req, resp := buildArithmeticCommand("incr", key, delta, c.options.noReply)
	if err := c.dispatchRequest(ctx, req, resp); err != nil {
		return 0, errors.Wrap(err, "request failed")
	}

	// parse response
	value, err := parseArithmetic(resp.rawLines[0])
	if err != nil {
		return 0, errors.Wrap(ErrMalformedResponse, err.Error())
	}

	return value, nil
}

func (c *client) Decr(ctx context.Context, key string, delta uint64) (uint64, error) {
	if err := validateKeyAndValue([]byte(key), nil); err != nil {
		return 0, err
	}

	req, resp := buildArithmeticCommand("decr", key, delta, c.options.noReply)
	if err := c.dispatchRequest(ctx, req, resp); err != nil {
		return 0, errors.Wrap(err, "request failed")
	}

	// parse response
	value, err := parseArithmetic(resp.rawLines[0])
	if err != nil {
		return 0, errors.Wrap(ErrMalformedResponse, err.Error())
	}

	return value, nil
}

func (c *client) Touch(ctx context.Context, key string, expiry uint32) error {
	if err := validateKeyAndValue([]byte(key), nil); err != nil {
		return err
	}

	req, resp := buildTouchCommand(key, expiry, c.options.noReply)
	defer releaseReqAndResp(req, resp)

	if err := c.dispatchRequest(ctx, req, resp); err != nil {
		return errors.Wrap(err, "request failed")
	}

	// expect TOUCHED\r\n
	if err := resp.expect(_TouchedCRLFBytes); err != nil {
		return errors.Wrap(ErrMalformedResponse, err.Error())
	}

	return nil
}

func (c *client) Version(ctx context.Context) (string, error) {
	req, resp := buildVersionCommand()
	defer releaseReqAndResp(req, resp)

	if err := c.dispatchRequest(ctx, req, resp); err != nil {
		return "", errors.Wrap(err, "request")
	}

	// parse version number from response
	// VERSION 1.6.14
	line := resp.rawLines[0]
	if !bytes.HasPrefix(line, _VersionBytes) {
		return "", errors.Wrap(ErrMalformedResponse, string(line))
	}

	return string(trimCRLF(line[8:])), nil
}

func (c *client) FlushAll(ctx context.Context) error {
	call := func(ctx context.Context, cn memcachedConn) error {
		req, resp := buildFlushAllCommand(c.options.noReply)
		defer releaseReqAndResp(req, resp)

		if err := req.send(ctx, cn, c.options.writeTimeout); err != nil {
			return errors.Wrap(err, "send failed")
		}
		if err := resp.recv(ctx, cn, c.options.readTimeout); err != nil {
			return errors.Wrap(err, "recv failed")
		}

		// expect OK\r\n
		if err := resp.expect(_OKCRLFBytes); err != nil {
			return errors.Wrap(ErrMalformedResponse, err.Error())
		}

		return nil
	}

	if err := c.broadcastRequest(ctx, call); err != nil {
		return errors.Wrap(err, "request failed")
	}

	return nil
}

/**
 * Other commands(META text commands):
 * meta set(ms), meta get(mg), meta delete(md), meta arithmetic(ma), meta no-op(mn)
 */

func (c *client) MetaSet(ctx context.Context, key, value []byte, msOptions ...MetaSetOption) (*MetaItem, error) {
	if err := validateKeyAndValue(key, nil); err != nil {
		return nil, err
	}

	msFlags := &metaSetFlags{}
	for _, applyFn := range msOptions {
		applyFn(msFlags)
	}

	req, resp := buildMetaSetCommand(key, value, msFlags)
	defer releaseReqAndResp(req, resp)
	if err := c.dispatchRequest(ctx, req, resp); err != nil {
		return nil, errors.Wrap(err, "request failed")
	}

	item := &MetaItem{
		Key:   key,
		TTL:   int64(msFlags.T),
		Flags: msFlags.F,
	}
	err := parseMetaItem(resp.rawLines, item, msFlags.q)
	if err != nil {
		return nil, err
	}

	return item, nil
}

func (c *client) MetaGet(ctx context.Context, key []byte, mgOptions ...MetaGetOption) (*MetaItem, error) {
	if err := validateKeyAndValue(key, nil); err != nil {
		return nil, err
	}

	mgFlags := &metaGetFlags{}
	for _, applyFn := range mgOptions {
		applyFn(mgFlags)
	}

	req, resp := buildMetaGetCommand(key, mgFlags)
	defer releaseReqAndResp(req, resp)

	if err := c.dispatchRequest(ctx, req, resp); err != nil {
		return nil, errors.Wrap(err, "request failed")
	}

	item := &MetaItem{
		Key: key,
	}
	if err := parseMetaItem(resp.rawLines, item, mgFlags.q); err != nil {
		return nil, err
	}

	return item, nil
}

func (c *client) MetaDelete(ctx context.Context, key []byte, options ...MetaDeleteOption) (*MetaItem, error) {
	if err := validateKeyAndValue(key, nil); err != nil {
		return nil, err
	}

	mdFlags := &metaDeleteFlags{}
	for _, applyFn := range options {
		applyFn(mdFlags)
	}

	req, resp := buildMetaDeleteCommand(key, mdFlags)
	defer releaseReqAndResp(req, resp)

	if err := c.dispatchRequest(ctx, req, resp); err != nil {
		return nil, errors.Wrap(err, "request failed")
	}

	item := &MetaItem{
		Key: key,
	}
	if err := parseMetaItem(resp.rawLines, item, mdFlags.q); err != nil {
		return nil, err
	}

	return item, nil
}

func (c *client) MetaArithmetic(ctx context.Context, key []byte, delta uint64, options ...MetaArithmeticOption) (*MetaItem, error) {
	if err := validateKeyAndValue(key, nil); err != nil {
		return nil, err
	}

	maFlags := &metaArithmeticFlags{}
	for _, applyFn := range options {
		applyFn(maFlags)
	}

	req, resp := buildMetaArithmeticCommand(key, delta, maFlags)
	defer releaseReqAndResp(req, resp)

	if err := c.dispatchRequest(ctx, req, resp); err != nil {
		return nil, errors.Wrap(err, "request failed")
	}

	item := &MetaItem{
		Key: key,
	}
	if err := parseMetaItem(resp.rawLines, item, maFlags.q); err != nil {
		return nil, err
	}

	return item, nil
}

func validateKeyAndValue(key, value []byte) error {
	if nKey := len(key); nKey == 0 || nKey > maxKeySize {
		return ErrInvalidKey
	}

	if nValue := len(value); nValue > maxValueSize {
		return ErrInvalidValue
	}

	return nil
}

func (c *client) MetaDebug(ctx context.Context, key []byte, options ...MetaDebugOption) (*MetaItemDebug, error) {
	if err := validateKeyAndValue(key, nil); err != nil {
		return nil, err
	}

	mdFlags := &metaDebugFlags{}
	for _, applyFn := range options {
		applyFn(mdFlags)
	}

	req, resp := buildMetaDebugCommand(key, mdFlags)
	defer releaseReqAndResp(req, resp)

	if err := c.dispatchRequest(ctx, req, resp); err != nil {
		return nil, errors.Wrap(err, "request failed")
	}

	item := &MetaItemDebug{
		Key: key,
	}
	// parse response
	if err := parseMetaItemDebug(resp.rawLines, item); err != nil {
		return nil, err
	}

	return item, nil
}

func (c *client) MetaNoOp(ctx context.Context) error {
	req, resp := buildMetaNoOpCommand()
	defer releaseReqAndResp(req, resp)

	if err := c.dispatchRequest(ctx, req, resp); err != nil {
		return errors.Wrap(err, "request failed")
	}
	if err := resp.expect(_MetaMNCRLFBytes); err != nil {
		if errors.Is(err, ErrMalformedResponse) {
			return err
		}

		return errors.Wrap(ErrMalformedResponse, err.Error())
	}

	return nil
}
