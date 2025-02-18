## memcached-cli

The memcached-cli is a simple command line interface to memcached.

### Features

#### Context Management
- Manage multiple memcached instance configurations
- Support create, delete, switch, and view contexts
- Each context contains:
  - Unique identifier
  - List of server addresses
  - Connection pool settings
  - Timeout configurations
  - SASL authentication info

#### Data Operations
- Basic Operations
  - get/gets: retrieve data
  - set/add/replace: store data
  - delete: remove data
  - incr/decr: increment/decrement values
  - touch: update expiration time
  - cas: atomic updates

#### Interactive Mode
- REPL interactive command line
- Command auto-completion
- Syntax highlighting
- Command history
- Help information display

### Usage

```bash
# Context Management
memcached-cli ctx create dev --servers="localhost:11211" --pool-size=5 # create a new context
memcached-cli ctx list    # list all contexts
memcached-cli ctx use dev # switch to context
memcached-cli ctx current # print current context

# Data Operations with current context
memcached-cli kv set mykey myvalue # set a key-value pair
memcached-cli kv get mykey         # get a key-value pair
memcached-cli kv delete mykey      # delete a key-value pair

# Data Operations with specific context
memcached-cli --context=prod set mykey myvalue

# other commands
memcached-cli version
memcached-cli flushall

# Interactive Mode
memcached-cli
```

You can use `memcached-cli -h` to see all available commands and options.
