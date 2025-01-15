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
