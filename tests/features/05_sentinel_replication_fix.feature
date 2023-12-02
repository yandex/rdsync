Feature: Sentinel mode broken replication fix

    Scenario: Sentinel mode broken shard with divergence in DCS and redis is fixed
        Given sentinel shard is up and running
        Then redis host "redis1" should be master
        And redis host "redis2" should become replica of "redis1" within "15" seconds
        And replication on redis host "redis2" should run fine within "15" seconds
        And redis host "redis3" should become replica of "redis1" within "15" seconds
        And replication on redis host "redis3" should run fine within "15" seconds
        And zookeeper node "/test/active_nodes" should match json_exactly within "30" seconds
        """
            ["redis1","redis2","redis3"]
        """
        When I run command on host "redis1"
        """
            supervisorctl signal STOP rdsync
        """
        And I run command on host "redis2"
        """
            supervisorctl signal STOP rdsync
        """
        And I run command on host "redis3"
        """
            supervisorctl signal STOP rdsync
        """
        When I run command on redis host "redis3"
        """
            REPLICAOF redis2 6379
        """
        Then redis cmd result should match regexp
        """
            .*OK.*
        """
        When I run command on redis host "redis2"
        """
            REPLICAOF NO ONE
        """
        Then redis cmd result should match regexp
        """
            .*OK.*
        """
        When I run command on redis host "redis1"
        """
            REPLICAOF redis2 6379
        """
        Then redis cmd result should match regexp
        """
            .*OK.*
        """
        When I run command on redis host "redis1"
        """
            CONFIG SET repl-paused yes
        """
        Then redis cmd result should match regexp
        """
            .*OK.*
        """
        When I run command on redis host "redis3"
        """
            CONFIG SET repl-paused yes
        """
        Then redis cmd result should match regexp
        """
            .*OK.*
        """
        When I run command on host "redis1"
        """
            supervisorctl signal CONT rdsync
        """
        And I run command on host "redis2"
        """
            supervisorctl signal CONT rdsync
        """
        And I run command on host "redis3"
        """
            supervisorctl signal CONT rdsync
        """
        Then zookeeper node "/test/master" should match json_exactly within "30" seconds
        """
            "redis2"
        """
        When I wait for "30" seconds
        And I run command on redis host "redis1"
        """
            CONFIG GET repl-paused
        """
        Then redis cmd result should match regexp
        """
            .*no.*
        """
        When I run command on redis host "redis3"
        """
            CONFIG GET repl-paused
        """
        Then redis cmd result should match regexp
        """
            .*no.*
        """

    Scenario: Sentinel mode master info divergence in DCS and redis is fixed
        Given sentinel shard is up and running
        Then redis host "redis1" should be master
        And redis host "redis2" should become replica of "redis1" within "15" seconds
        And replication on redis host "redis2" should run fine within "15" seconds
        And redis host "redis3" should become replica of "redis1" within "15" seconds
        And replication on redis host "redis3" should run fine within "15" seconds
        And zookeeper node "/test/active_nodes" should match json_exactly within "30" seconds
        """
            ["redis1","redis2","redis3"]
        """
        When I set zookeeper node "/test/master" to
        """
            "redis3"
        """
        Then zookeeper node "/test/master" should match json_exactly within "30" seconds
        """
            "redis1"
        """

    Scenario: Sentinel mode nonexistent master info in DCS is fixed
        Given sentinel shard is up and running
        Then redis host "redis1" should be master
        And redis host "redis2" should become replica of "redis1" within "15" seconds
        And replication on redis host "redis2" should run fine within "15" seconds
        And redis host "redis3" should become replica of "redis1" within "15" seconds
        And replication on redis host "redis3" should run fine within "15" seconds
        And zookeeper node "/test/active_nodes" should match json_exactly within "30" seconds
        """
            ["redis1","redis2","redis3"]
        """
        When I set zookeeper node "/test/master" to
        """
            "this_host_does_not_exist"
        """
        Then zookeeper node "/test/master" should match json_exactly within "30" seconds
        """
            "redis1"
        """

    Scenario: Sentinel mode accidential cascade replication is fixed
        Given sentinel shard is up and running
        Then redis host "redis1" should be master
        And redis host "redis2" should become replica of "redis1" within "15" seconds
        And replication on redis host "redis2" should run fine within "15" seconds
        And redis host "redis3" should become replica of "redis1" within "15" seconds
        And replication on redis host "redis3" should run fine within "15" seconds
        And zookeeper node "/test/active_nodes" should match json_exactly within "30" seconds
        """
            ["redis1","redis2","redis3"]
        """
        When I run command on redis host "redis3"
        """
            REPLICAOF redis2 6379
        """
        Then redis cmd result should match regexp
        """
            OK
        """
        And redis host "redis3" should become replica of "redis1" within "60" seconds
        And replication on redis host "redis3" should run fine within "15" seconds

    Scenario: Sentinel mode replication pause on replica is fixed
        Given sentinel shard is up and running
        Then redis host "redis1" should be master
        And redis host "redis2" should become replica of "redis1" within "15" seconds
        And replication on redis host "redis2" should run fine within "15" seconds
        And redis host "redis3" should become replica of "redis1" within "15" seconds
        And replication on redis host "redis3" should run fine within "15" seconds
        And zookeeper node "/test/active_nodes" should match json_exactly within "30" seconds
        """
            ["redis1","redis2","redis3"]
        """
        When I break replication on host "redis3"
        Then redis host "redis3" should become replica of "redis1" within "15" seconds
        And replication on redis host "redis3" should run fine within "60" seconds
