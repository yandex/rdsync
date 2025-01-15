Feature: Sentinel mode survives dcs conn loss

    Scenario: Sentinel mode survives dcs conn loss
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
        When host "zoo3" is detached from the network
        And host "zoo2" is detached from the network
        And host "zoo1" is detached from the network
        Then valkey host "valkey1" should be master
        And valkey host "valkey2" should become replica of "valkey1" within "15" seconds
        And replication on valkey host "valkey2" should run fine within "15" seconds
        And valkey host "valkey3" should become replica of "valkey1" within "15" seconds
        And replication on valkey host "valkey3" should run fine within "15" seconds
        When I run command on valkey host "valkey1"
        """
            SET MYKEY TESTVALUE
        """
        Then valkey cmd result should match regexp
        """
            OK
        """

    Scenario: Sentinel mode partitioned master goes offline
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
        When host "zoo3" is detached from the network
        And host "zoo2" is detached from the network
        And host "zoo1" is detached from the network
        And host "valkey2" is detached from the network
        And host "valkey3" is detached from the network
        Then valkey host "valkey1" should become unavailable within "60" seconds
        When host "zoo3" is attached to the network
        And host "zoo2" is attached to the network
        And host "zoo1" is attached to the network
        And host "valkey2" is attached to the network
        And host "valkey3" is attached to the network
        Then valkey host "valkey1" should become available within "60" seconds

    Scenario: Sentinel mode partitioned replica goes offline
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
        When host "zoo3" is detached from the network
        And host "zoo2" is detached from the network
        And host "zoo1" is detached from the network
        And host "valkey1" is detached from the network
        And host "valkey3" is detached from the network
        Then valkey host "valkey2" should become unavailable within "60" seconds
        When host "zoo3" is attached to the network
        And host "zoo2" is attached to the network
        And host "zoo1" is attached to the network
        And host "valkey1" is attached to the network
        And host "valkey3" is attached to the network
        Then valkey host "valkey2" should become available within "60" seconds

    Scenario: Sentinel mode partially partitioned manager gives up on manager role
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
        When I run command on host "valkey1" with timeout "20" seconds
        """
            supervisorctl stop rdsync
        """
        Then command return code should be "0"
        And  zookeeper node "/test/manager" should match regexp within "30" seconds
        """
            .*valkey[23].*
        """
        When I run command on host "valkey1" with timeout "20" seconds
        """
            supervisorctl start rdsync
        """
        When I get zookeeper node "/test/manager"
        And I save zookeeper query result as "new_manager"
        And port "6379" on host "{{.new_manager.hostname}}" is blocked
        And I wait for "120" seconds
        Then valkey host "valkey1" should be master
        When I run command on host "{{.new_manager.hostname}}"
        """
            grep ERROR /var/log/rdsync.log
        """
        Then command output should match regexp
        """
            .*Giving up on manager role.*
        """
        When I run command on host "{{.new_manager.hostname}}"
        """
            grep INFO /var/log/rdsync.log
        """
        Then command output should match regexp
        """
            .*New manager.*
        """
        When port "6379" on host "{{.new_manager.hostname}}" is unblocked
        Then zookeeper node "/test/active_nodes" should match json_exactly within "30" seconds
        """
            ["valkey1","valkey2","valkey3"]
        """

    Scenario: Sentinel mode partially partitioned manager gives up on manager role and triggers failover on master
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
        When port "6379" on host "valkey1" is blocked
        And I wait for "240" seconds
        And I run command on host "valkey1"
        """
            grep ERROR /var/log/rdsync.log
        """
        Then command output should match regexp
        """
            .*Giving up on manager role.*
        """
        And zookeeper node "/test/last_switch" should match json within "60" seconds
        """
        {
            "cause": "auto",
            "from": "valkey1",
            "result": {
                "ok": true
            }
        }
        """
        When I get zookeeper node "/test/master"
        And I save zookeeper query result as "new_master"
        Then valkey host "{{.new_master}}" should be master
        When port "6379" on host "valkey1" is unblocked
        Then zookeeper node "/test/active_nodes" should match json_exactly within "30" seconds
        """
            ["valkey1","valkey2","valkey3"]
        """
