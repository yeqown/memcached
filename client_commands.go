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
	Set(ctx context.Context, key, value string, flags, expiry uint32) error
	// Touch is used to update the expiration time of an existing item
	// without fetching it.
	//
	// expiry is the TTL of the key in seconds.
	Touch(ctx context.Context, key string, expiry uint32) error
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

	/**
	Delete commands: delete
	*/

	// Delete is used to delete the given key.
	Delete(ctx context.Context, key string) error
}

type metaTextProtocolCommander interface {
	// TODO: add and implement more meta commands

	MetaSet(ctx context.Context, key string) error
	MetaGet(ctx context.Context, key string) (*Item, error)
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
	if !bytes.HasPrefix(resp.raw, _VersionBytes) {
		return "", errors.Wrap(ErrMalformedResponse, string(resp.raw))
	}

	return string(trimCRLF(resp.raw[8:])), nil
}

/**
 * BASIC text commands:
	storage:   set, add, replace, append, prepend, cas
	retrieval: get, gets
	deletion:  delete
*/

func (c *client) Set(ctx context.Context, key, value string, flags, expiry uint32) error {
	req, resp := buildStorageCommand("set", key, []byte(value), flags, expiry, c.options.noReply)
	if err := c.doRequest(ctx, req, resp); err != nil {
		return errors.Wrap(err, "request failed")
	}

	// expect STORED\r\n
	if err := resp.expect(_StoredCRLFBytes); err != nil {
		return errors.Wrap(ErrMalformedResponse, err.Error())
	}

	return nil
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

func (c *client) Get(ctx context.Context, key string) (*Item, error) {
	req, resp := buildGetCommand(key)
	if err := c.doRequest(ctx, req, resp); err != nil {
		return nil, errors.Wrap(err, "request failed")
	}

	// parse response
	items, err := parseValueItems(resp.raw)
	if err != nil {
		return nil, errors.Wrap(ErrMalformedResponse, err.Error())
	}
	if len(items) == 0 {
		return nil, errors.Wrap(ErrNotFound, "no items found")
	}

	return items[0], nil
}

func (c *client) Gets(ctx context.Context, keys ...string) ([]*Item, error) {
	req, resp := buildGetsCommand(keys...)
	if err := c.doRequest(ctx, req, resp); err != nil {
		return nil, errors.Wrap(err, "request failed")
	}

	// parse response
	items, err := parseValueItems(resp.raw)
	if err != nil {
		return nil, errors.Wrap(ErrMalformedResponse, err.Error())
	}
	if len(items) == 0 {
		return nil, errors.Wrap(ErrNotFound, "no items found")
	}

	return items, nil
}

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

/**
 * META text commands: meta set(ms), meta get(mg), meta delete(md), meta arithmetic(ma), meta no-op(mn)
 */

func (c *client) MetaSet(ctx context.Context, key string) error {
	panic("implement me")
}

func (c *client) MetaGet(ctx context.Context, key string) (*Item, error) {
	panic("implement me")
}
