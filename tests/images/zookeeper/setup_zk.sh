#!/bin/bash

set -xe

retriable_path_create.sh /test
retriable_path_create.sh /test/ha_nodes
retriable_path_create.sh /test/ha_nodes/valkey1 set_priority
retriable_path_create.sh /test/ha_nodes/valkey2 set_priority
retriable_path_create.sh /test/ha_nodes/valkey3 set_priority
