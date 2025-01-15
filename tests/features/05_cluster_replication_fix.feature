Feature: Cluster mode broken replication fix

    Scenario: Cluster mode broken shard with divergence in DCS and valkey is fixed
        Given clustered shard is up and running
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
        When I run command on valkey host "valkey2"
        """
            CLUSTER FAILOVER
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

    Scenario: Cluster mode master info divergence in DCS and valkey is fixed
        Given clustered shard is up and running
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

    Scenario: Cluster mode nonexistent master info in DCS is fixed
        Given clustered shard is up and running
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

    Scenario: Cluster mode accidental cascade replication is fixed
        Given clustered shard is up and running
        Then valkey host "valkey1" should be master
        And valkey host "valkey2" should become replica of "valkey1" within "15" seconds
        And replication on valkey host "valkey2" should run fine within "15" seconds
        And valkey host "valkey3" should become replica of "valkey1" within "15" seconds
        And replication on valkey host "valkey3" should run fine within "15" seconds
        And zookeeper node "/test/active_nodes" should match json_exactly within "30" seconds
        """
            ["valkey1","valkey2","valkey3"]
        """
        When I run command on host "valkey3"
        """
            setup_cluster.sh valkey2
        """
        Then command return code should be "0"
        And valkey host "valkey3" should become replica of "valkey1" within "60" seconds
        And replication on valkey host "valkey3" should run fine within "15" seconds

    Scenario: Cluster mode replication pause on replica is fixed
        Given clustered shard is up and running
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

    Scenario: Cluster lone node is joined in cluster back
        Given clustered shard is up and running
        Then valkey host "valkey1" should be master
        And valkey host "valkey2" should become replica of "valkey1" within "15" seconds
        And replication on valkey host "valkey2" should run fine within "15" seconds
        And valkey host "valkey3" should become replica of "valkey1" within "15" seconds
        And replication on valkey host "valkey3" should run fine within "15" seconds
        And zookeeper node "/test/active_nodes" should match json_exactly within "30" seconds
        """
            ["valkey1","valkey2","valkey3"]
        """
        When I run command on host "valkey3"
        """
            rm -f /etc/valkey/cluster.conf
        """
        And I run command on host "valkey3"
        """
            sed -i -e 's/offline yes/offline no/' /etc/valkey/valkey.conf
        """
        And I run command on host "valkey3"
        """
            supervisorctl signal KILL valkey
        """
        And I run command on host "valkey3"
        """
            supervisorctl start valkey
        """
        Then valkey host "valkey3" should become replica of "valkey1" within "15" seconds
        And replication on valkey host "valkey3" should run fine within "15" seconds
        And zookeeper node "/test/active_nodes" should match json_exactly within "30" seconds
        """
            ["valkey1","valkey2","valkey3"]
        """

    Scenario: Cluster splitbrain is fixed in favor of node with slots
        Given clustered shard is up and running
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
        And I run command on host "valkey3"
        """
            rm -f /etc/valkey/cluster.conf
        """
        And I run command on host "valkey3"
        """
            sed -i -e 's/offline yes/offline no/' /etc/valkey/valkey.conf
        """
        And I run command on host "valkey3"
        """
            supervisorctl signal KILL valkey
        """
        And I run command on host "valkey3"
        """
            supervisorctl start valkey
        """
        Then valkey host "valkey3" should become available within "60" seconds
        When I run command on valkey host "valkey1"
        """
            SET very-important-key foo
        """
        And I set zookeeper node "/test/master" to
        """
            "valkey3"
        """
        And I run command on host "valkey1"
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
        Then valkey host "valkey3" should become replica of "valkey1" within "60" seconds
        And replication on valkey host "valkey3" should run fine within "15" seconds
        And zookeeper node "/test/active_nodes" should match json_exactly within "30" seconds
        """
            ["valkey1","valkey2","valkey3"]
        """
        When I run command on valkey host "valkey1"
        """
            GET very-important-key
        """
        Then valkey cmd result should match regexp
        """
            .*foo.*
        """
