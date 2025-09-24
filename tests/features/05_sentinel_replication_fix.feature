Feature: Sentinel mode broken replication fix

    Scenario: Sentinel mode broken shard with divergence in DCS and valkey is fixed
        Given sentinel shard is up and running
        Then valkey host "valkey1" should be master
        And valkey host "valkey2" should become replica of "valkey1" within "15" seconds
        And replication on valkey host "valkey2" should run fine within "15" seconds
        And valkey host "valkey3" should become replica of "valkey1" within "15" seconds
        And replication on valkey host "valkey3" should run fine within "15" seconds
        And zookeeper node "/test/active_nodes" should match json_exactly within "30" seconds
        """
            ["valkey1","valkey2","valkey3"]
        """
        When I run command on host "valkey1"
        """
            supervisorctl signal STOP rdsync
        """
        And I run command on host "valkey2"
        """
            supervisorctl signal STOP rdsync
        """
        And I run command on host "valkey3"
        """
            supervisorctl signal STOP rdsync
        """
        When I run command on valkey host "valkey3"
        """
            REPLICAOF valkey2 6379
        """
        Then valkey cmd result should match regexp
        """
            .*OK.*
        """
        When I run command on valkey host "valkey2"
        """
            REPLICAOF NO ONE
        """
        Then valkey cmd result should match regexp
        """
            .*OK.*
        """
        When I run command on valkey host "valkey1"
        """
            REPLICAOF valkey2 6379
        """
        Then valkey cmd result should match regexp
        """
            .*OK.*
        """
        When I run command on valkey host "valkey1"
        """
            CONFIG SET repl-paused yes
        """
        Then valkey cmd result should match regexp
        """
            .*OK.*
        """
        When I run command on valkey host "valkey3"
        """
            CONFIG SET repl-paused yes
        """
        Then valkey cmd result should match regexp
        """
            .*OK.*
        """
        When I run command on host "valkey1"
        """
            supervisorctl signal CONT rdsync
        """
        And I run command on host "valkey2"
        """
            supervisorctl signal CONT rdsync
        """
        And I run command on host "valkey3"
        """
            supervisorctl signal CONT rdsync
        """
        Then zookeeper node "/test/master" should match json_exactly within "30" seconds
        """
            "valkey2"
        """
        When I wait for "30" seconds
        And I run command on valkey host "valkey1"
        """
            CONFIG GET repl-paused
        """
        Then valkey cmd result should match regexp
        """
            .*no.*
        """
        When I run command on valkey host "valkey3"
        """
            CONFIG GET repl-paused
        """
        Then valkey cmd result should match regexp
        """
            .*no.*
        """

    Scenario: Sentinel mode master info divergence in DCS and valkey is fixed
        Given sentinel shard is up and running
        Then valkey host "valkey1" should be master
        And valkey host "valkey2" should become replica of "valkey1" within "15" seconds
        And replication on valkey host "valkey2" should run fine within "15" seconds
        And valkey host "valkey3" should become replica of "valkey1" within "15" seconds
        And replication on valkey host "valkey3" should run fine within "15" seconds
        And zookeeper node "/test/active_nodes" should match json_exactly within "30" seconds
        """
            ["valkey1","valkey2","valkey3"]
        """
        When I set zookeeper node "/test/master" to
        """
            "valkey3"
        """
        Then zookeeper node "/test/master" should match json_exactly within "30" seconds
        """
            "valkey1"
        """

    Scenario: Sentinel mode nonexistent master info in DCS is fixed
        Given sentinel shard is up and running
        Then valkey host "valkey1" should be master
        And valkey host "valkey2" should become replica of "valkey1" within "15" seconds
        And replication on valkey host "valkey2" should run fine within "15" seconds
        And valkey host "valkey3" should become replica of "valkey1" within "15" seconds
        And replication on valkey host "valkey3" should run fine within "15" seconds
        And zookeeper node "/test/active_nodes" should match json_exactly within "30" seconds
        """
            ["valkey1","valkey2","valkey3"]
        """
        When I set zookeeper node "/test/master" to
        """
            "this_host_does_not_exist"
        """
        Then zookeeper node "/test/master" should match json_exactly within "30" seconds
        """
            "valkey1"
        """

    Scenario: Sentinel mode accidental cascade replication is fixed
        Given sentinel shard is up and running
        Then valkey host "valkey1" should be master
        And valkey host "valkey2" should become replica of "valkey1" within "15" seconds
        And replication on valkey host "valkey2" should run fine within "15" seconds
        And valkey host "valkey3" should become replica of "valkey1" within "15" seconds
        And replication on valkey host "valkey3" should run fine within "15" seconds
        And zookeeper node "/test/active_nodes" should match json_exactly within "30" seconds
        """
            ["valkey1","valkey2","valkey3"]
        """
        When I run command on valkey host "valkey3"
        """
            REPLICAOF valkey2 6379
        """
        Then valkey cmd result should match regexp
        """
            OK
        """
        And valkey host "valkey3" should become replica of "valkey1" within "60" seconds
        And replication on valkey host "valkey3" should run fine within "15" seconds

    Scenario: Sentinel mode replication pause on replica is fixed
        Given sentinel shard is up and running
        Then valkey host "valkey1" should be master
        And valkey host "valkey2" should become replica of "valkey1" within "15" seconds
        And replication on valkey host "valkey2" should run fine within "15" seconds
        And valkey host "valkey3" should become replica of "valkey1" within "15" seconds
        And replication on valkey host "valkey3" should run fine within "15" seconds
        And zookeeper node "/test/active_nodes" should match json_exactly within "30" seconds
        """
            ["valkey1","valkey2","valkey3"]
        """
        When I break replication on host "valkey3"
        Then valkey host "valkey3" should become replica of "valkey1" within "15" seconds
        And replication on valkey host "valkey3" should run fine within "60" seconds

    Scenario: Sentinel mode stale replica goes offline
        Given sentinel shard is up and running
        Then valkey host "valkey1" should be master
        And valkey host "valkey2" should become replica of "valkey1" within "15" seconds
        And replication on valkey host "valkey2" should run fine within "15" seconds
        And valkey host "valkey3" should become replica of "valkey1" within "15" seconds
        And replication on valkey host "valkey3" should run fine within "15" seconds
        And zookeeper node "/test/active_nodes" should match json_exactly within "30" seconds
        """
            ["valkey1","valkey2","valkey3"]
        """
        When host "valkey1" and port "6379" on host "valkey3" is blocked
        Then valkey host "valkey3" should become unavailable within "240" seconds
        When host "valkey1" and port "6379" on host "valkey3" is unblocked
        Then valkey host "valkey3" should become replica of "valkey1" within "30" seconds
        And replication on valkey host "valkey3" should run fine within "15" seconds

    Scenario: Sentinel mode destructive replication repair works
        Given sentinel shard is up and running
        Then valkey host "valkey1" should be master
        And valkey host "valkey2" should become replica of "valkey1" within "15" seconds
        And replication on valkey host "valkey2" should run fine within "15" seconds
        And valkey host "valkey3" should become replica of "valkey1" within "15" seconds
        And replication on valkey host "valkey3" should run fine within "15" seconds
        And zookeeper node "/test/active_nodes" should match json_exactly within "30" seconds
        """
            ["valkey1","valkey2","valkey3"]
        """
        When I run command on host "valkey2" with timeout "20" seconds
        """
            supervisorctl stop rdsync
        """
        And I run command on host "valkey2" with timeout "20" seconds
        """
            supervisorctl stop valkey
        """
        And I run command on host "valkey2"
        """
            rm -rf /var/lib/valkey/appendonlydir
        """
        And I run command on host "valkey2"
        """
            truncate -s 0 /var/lib/valkey/dump.rdb
        """
        And I run command on host "valkey2"
        """
            chattr +i /var/lib/valkey/dump.rdb
        """
        And I run command on host "valkey2" with timeout "20" seconds
        """
            supervisorctl start valkey
        """
        And I run command on host "valkey2" with timeout "20" seconds
        """
            supervisorctl start rdsync
        """
        Then replication on valkey host "valkey2" should run fine within "600" seconds

    Scenario: Sentinel mode single replica is promoted
        Given sentinel shard is up and running
        Then valkey host "valkey1" should be master
        When host "valkey3" is deleted
        Then valkey host "valkey3" should become unavailable within "10" seconds
        When host "valkey2" is deleted
        Then valkey host "valkey2" should become unavailable within "10" seconds
        And zookeeper node "/test/active_nodes" should match json_exactly within "30" seconds
        """
            ["valkey1"]
        """
        And valkey host "valkey1" should be master
        When I run command on host "valkey1" with timeout "20" seconds
        """
            supervisorctl stop rdsync
        """
        And I run command on host "valkey1" with timeout "20" seconds
        """
            supervisorctl stop valkey
        """
        And I run command on host "valkey1" with timeout "20" seconds
        """
            echo 'replicaof 192.168.234.13 6379' >> /etc/valkey/valkey.conf
        """
        And I run command on host "valkey1" with timeout "20" seconds
        """
            supervisorctl start valkey
        """
        And I run command on host "valkey1" with timeout "20" seconds
        """
            supervisorctl start rdsync
        """
        Then valkey host "valkey1" should become available within "60" seconds
        And valkey host "valkey1" should be master
