package memcached

import (
	"bytes"
	"context"

	"github.com/pkg/errors"
)

// Item represents a key-value pair to be got or stored.
type Item struct {
	Key   string
	Value []byte
	Flags uint32
	// CASUnique is a unique value that is used to check-and-set operation.
	// It ONLY returns when you use `Gets` command.
	CASUnique uint64
}

// MetaItem represents a key-value pair with meta information.
type MetaItem struct {
	Key   string
	Value []byte

	// Flags is the flags of the value.
	// TODO(@yeqown): define flags here.
}

type basicTextProtocolCommander interface {
	/**
	Authentication commands: auth
	*/

	// Auth is used to authenticate the client to the server.
	Auth(ctx context.Context, username, password string) error

	/**
	Storage commands: set, add, replace, append, prepend, cas
	*/

	// Set is used to store the given key-value pair.
	//
	// flags is an arbitrary 32-bit unsigned integer (written out in decimal) that
	// the server stores along with the data and sends back when the item is retrieved.
	//
	// expiry is the TTL of the key in seconds.
	Set(ctx context.Context, key, value string, flags, expiry uint32) error
	// Add is used to store the given key-value pair if the key does not exist.
	//
	// flags is an arbitrary 32-bit unsigned integer (written out in decimal) that
	// the server stores along with the data and sends back when the item is retrieved.
	//
	// expiry is the TTL of the key in seconds.
	Add(ctx context.Context, key, value string, flags, expiry uint32) error
	// Replace is used to update the value of an existing item.
	//
	// flags is an arbitrary 32-bit unsigned integer (written out in decimal) that
	// the server stores along with the data and sends back when the item is retrieved.
	//
	// expiry is the TTL of the key in seconds.
	Replace(ctx context.Context, key, value string, flags, expiry uint32) error
	// Append is used to append the value to an existing item.
	//
	// flags is an arbitrary 32-bit unsigned integer (written out in decimal) that
	// the server stores along with the data and sends back when the item is retrieved.
	//
	// expiry is the TTL of the key in seconds.
	Append(ctx context.Context, key, value string, flags, expiry uint32) error
	// Prepend is used to prepend the value to an existing item.
	//
	// flags is an arbitrary 32-bit unsigned integer (written out in decimal) that
	// the server stores along with the data and sends back when the item is retrieved.
	//
	// expiry is the TTL of the key in seconds.
	Prepend(ctx context.Context, key, value string, flags, expiry uint32) error

	// Cas is used to update the value of an existing item and also check-and-set operation.
	//
	// flags is an arbitrary 32-bit unsigned integer (written out in decimal) that
	// the server stores along with the data and sends back when the item is retrieved.
	//
	// expiry is the TTL of the key in seconds.
	Cas(ctx context.Context, key, value string, flags, expiry uint32, cas uint64) error

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
}

type metaTextProtocolCommander interface {
	// TODO(@yeqown): add and implement more meta commands

	MetaSet(ctx context.Context, key string, value []byte, options ...MetaSetOption) (*MetaItem, error)
	MetaGet(ctx context.Context, key string, options ...MetaGetOption) (*MetaItem, error)
}

type statisticsTextProtocolCommander interface {
	// TODO: add more statistics commands

	Stats(ctx context.Context, args ...string) (map[string]string, error)
}

func (c *client) Version(ctx context.Context) (string, error) {
	req := buildVersionCommand()
	resp := buildLimitedLineResponse(1)
	if err := c.doRequest(ctx, req, resp); err != nil {
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

/**
 * Authentication commands: auth
 */

func (c *client) Auth(ctx context.Context, username, password string) error {
	// TODO(@yeqown): test this API and figure out the SASL authentication mechanism.
	req, resp := buildAuthCommand(username, password)
	if err := c.doRequest(ctx, req, resp); err != nil {
		return errors.Wrap(err, "request failed")
	}

	// expect STORED\r\n
	if err := resp.expect(_StoredCRLFBytes); err != nil {
		return errors.Wrap(ErrMalformedResponse, err.Error())
	}

	return nil
}

/**
 * Storage commands: set, add, replace, append, prepend, cas
 */

func (c *client) storageCommand(ctx context.Context, command, key, value string, flags, expiry uint32) error {
	req, resp := buildStorageCommand(command, key, []byte(value), flags, expiry, c.options.noReply)
	if err := c.doRequest(ctx, req, resp); err != nil {
		return errors.Wrap(err, "request failed")
	}

	// expect STORED\r\n
	if err := resp.expect(_StoredCRLFBytes); err != nil {
		return errors.Wrap(ErrMalformedResponse, err.Error())
	}

	return nil
}

func (c *client) Set(ctx context.Context, key, value string, flags, expiry uint32) error {
	return c.storageCommand(ctx, "set", key, value, flags, expiry)
}

func (c *client) Add(ctx context.Context, key, value string, flags, expiry uint32) error {
	return c.storageCommand(ctx, "add", key, value, flags, expiry)
}

func (c *client) Replace(ctx context.Context, key, value string, flags, expiry uint32) error {
	return c.storageCommand(ctx, "replace", key, value, flags, expiry)
}

func (c *client) Append(ctx context.Context, key, value string, flags, expiry uint32) error {
	return c.storageCommand(ctx, "append", key, value, flags, expiry)
}

func (c *client) Prepend(ctx context.Context, key, value string, flags, expiry uint32) error {
	return c.storageCommand(ctx, "prepend", key, value, flags, expiry)
}

func (c *client) Cas(ctx context.Context, key, value string, flags, expiry uint32, cas uint64) error {
	req, resp := buildCasCommand(key, []byte(value), flags, expiry, cas, c.options.noReply)
	if err := c.doRequest(ctx, req, resp); err != nil {
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
	req, resp := buildGetsCommand("get", key)
	if err := c.doRequest(ctx, req, resp); err != nil {
		return nil, errors.Wrap(err, "request failed")
	}

	// parse response
	items, err := parseValueItems(resp.rawLines, false)
	if err != nil {
		return nil, errors.Wrap(err, "parse values failed")
	}
	if len(items) == 0 {
		return nil, errors.Wrap(ErrNotFound, "no items found")
	}

	return items[0], nil
}

func (c *client) Gets(ctx context.Context, keys ...string) ([]*Item, error) {
	req, resp := buildGetsCommand("gets", keys...)
	if err := c.doRequest(ctx, req, resp); err != nil {
		return nil, errors.Wrap(err, "request failed")
	}

	// parse response
	items, err := parseValueItems(resp.rawLines, false)
	if err != nil {
		return nil, errors.Wrap(ErrMalformedResponse, "parse values failed")
	}
	if len(items) == 0 {
		return nil, errors.Wrap(ErrNotFound, "no items found")
	}

	return items, nil
}

func (c *client) GetAndTouch(ctx context.Context, expiry uint32, key string) (*Item, error) {
	req, resp := buildGetAndTouchesCommand("gat", expiry, key)
	if err := c.doRequest(ctx, req, resp); err != nil {
		return nil, errors.Wrap(err, "request failed")
	}

	// parse response
	items, err := parseValueItems(resp.rawLines, false)
	if err != nil {
		return nil, errors.Wrap(ErrMalformedResponse, "parse values failed")
	}

	if len(items) == 0 {
		return nil, errors.Wrap(ErrNotFound, "no items found")
	}

	return items[0], nil
}

func (c *client) GetAndTouches(ctx context.Context, expiry uint32, keys ...string) ([]*Item, error) {
	req, resp := buildGetAndTouchesCommand("gats", expiry, keys...)
	if err := c.doRequest(ctx, req, resp); err != nil {
		return nil, errors.Wrap(err, "request failed")
	}

	// parse response
	items, err := parseValueItems(resp.rawLines, false)
	if err != nil {
		return nil, errors.Wrap(ErrMalformedResponse, "parse values failed")
	}

	if len(items) == 0 {
		return nil, errors.Wrap(ErrNotFound, "no items found")
	}

	return items, nil
}

/**
 * Other commands: delete, incr, decr, touch
 */

func (c *client) Delete(ctx context.Context, key string) error {
	req, resp := buildDeleteCommand(key, c.options.noReply)
	if err := c.doRequest(ctx, req, resp); err != nil {
		return errors.Wrap(err, "request failed")
	}

	// expect DELETED\r\n
	if err := resp.expect(_DeletedCRLFBytes); err != nil {
		return errors.Wrap(ErrMalformedResponse, err.Error())
	}

	return nil
}

func (c *client) Incr(ctx context.Context, key string, delta uint64) (uint64, error) {
	req, resp := buildArithmeticCommand("incr", key, delta, c.options.noReply)
	if err := c.doRequest(ctx, req, resp); err != nil {
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
	req, resp := buildArithmeticCommand("decr", key, delta, c.options.noReply)
	if err := c.doRequest(ctx, req, resp); err != nil {
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
	req, resp := buildTouchCommand(key, expiry, c.options.noReply)
	if err := c.doRequest(ctx, req, resp); err != nil {
		return errors.Wrap(err, "request failed")
	}

	// expect TOUCHED\r\n
	if err := resp.expect(_TouchedCRLFBytes); err != nil {
		return errors.Wrap(ErrMalformedResponse, err.Error())
	}

	return nil
}

/**
 * Other commands(META text commands):
 * meta set(ms), meta get(mg), meta delete(md), meta arithmetic(ma), meta no-op(mn)
 */

func (c *client) MetaSet(ctx context.Context, key string, value []byte, msOptions ...MetaSetOption) (*MetaItem, error) {
	msFlags := &metaSetFlags{}
	for _, applyFn := range msOptions {
		applyFn(msFlags)
	}

	req, resp := buildMetaSetCommand(key, value, msFlags)
	if err := c.doRequest(ctx, req, resp); err != nil {
		return nil, errors.Wrap(err, "request failed")
	}

	// expect HD flags\r\n
	// TODO: handle meta set response
	panic("not implemented")

}

func (c *client) MetaGet(ctx context.Context, key string, mgOptions ...MetaGetOption) (*MetaItem, error) {
	mgFlags := &metaGetFlags{}
	for _, applyFn := range mgOptions {
		applyFn(mgFlags)
	}

	req, resp := buildMetaGetCommand(key, mgFlags)
	if err := c.doRequest(ctx, req, resp); err != nil {
		return nil, errors.Wrap(err, "request failed")
	}

	// TODO: handle mete get response
	panic("not implemented")

}
