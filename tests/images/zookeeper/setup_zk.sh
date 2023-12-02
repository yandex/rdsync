#!/bin/bash

set -xe

retriable_path_create.sh /test
retriable_path_create.sh /test/ha_nodes
retriable_path_create.sh /test/ha_nodes/redis1 set_priority
retriable_path_create.sh /test/ha_nodes/redis2 set_priority
retriable_path_create.sh /test/ha_nodes/redis3 set_priority
