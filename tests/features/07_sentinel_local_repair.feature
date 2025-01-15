Feature: Sentinel mode local node repair

    Scenario: Sentinel mode senticache is restarted after OOM
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
            supervisorctl stop senticache
        """
        Then senticache host "valkey1" should have master "valkey1" within "30" seconds

    Scenario: Sentinel mode replica is restarted after OOM
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
        When valkey on host "valkey2" is killed
        And I wait for "300" seconds
        Then valkey host "valkey2" should become available within "120" seconds
        And valkey host "valkey2" should become replica of "valkey1" within "60" seconds
        And replication on valkey host "valkey2" should run fine within "60" seconds

    Scenario: Sentinel mode master is restarted after hanging
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
        When I run async command on host "valkey1"
        """
            valkey-cli -a functestpassword DEBUG SLEEP 600
        """
        And I wait for "420" seconds
        Then valkey host "valkey1" should become available within "60" seconds
