Feature: Sentinel mode smoke tests

    Scenario: Sentinel mode initially works
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
        And senticache host "valkey1" should have master "valkey1" within "30" seconds
        And senticache host "valkey2" should have master "valkey1" within "30" seconds
        And senticache host "valkey3" should have master "valkey1" within "30" seconds
        And path "/var/lib/valkey/appendonlydir" does not exist on "valkey1"
        And path "/var/lib/valkey/appendonlydir" exists on "valkey2"
        And path "/var/lib/valkey/appendonlydir" exists on "valkey3"

    Scenario: Sentinel mode duplicate ip resolve does not break rdsync
        Given sentinel shard is up and running
        When I run command on host "valkey1"
        """
            echo '192.168.234.14 valkey2 test1' >> /etc/hosts
            echo '192.168.234.14 valkey2 test2' >> /etc/hosts
            echo '192.168.234.15 valkey3 test3' >> /etc/hosts
            echo '192.168.234.15 valkey3 test4' >> /etc/hosts
        """
        Then valkey host "valkey1" should be master
        And valkey host "valkey2" should become replica of "valkey1" within "15" seconds
        And replication on valkey host "valkey2" should run fine within "15" seconds
        And valkey host "valkey3" should become replica of "valkey1" within "15" seconds
        And replication on valkey host "valkey3" should run fine within "15" seconds
        And zookeeper node "/test/active_nodes" should match json_exactly within "30" seconds
        """
            ["valkey1","valkey2","valkey3"]
        """
        And senticache host "valkey1" should have master "valkey1" within "30" seconds
        And senticache host "valkey2" should have master "valkey1" within "30" seconds
        And senticache host "valkey3" should have master "valkey1" within "30" seconds
        When I run command on host "valkey3"
        """
            supervisorctl stop rdsync
        """
        And I run command on host "valkey2"
        """
            supervisorctl stop rdsync
        """
        And I run command on host "valkey1"
        """
            supervisorctl stop rdsync
        """
        And I run command on valkey host "valkey1"
        """
            CONFIG SET quorum-replicas valkey2:6379
        """
        And I run command on host "valkey1"
        """
            supervisorctl start rdsync
        """
        And I run command on host "valkey2"
        """
            supervisorctl start rdsync
        """
        And I run command on host "valkey3"
        """
            supervisorctl start rdsync
        """
        When I set zookeeper node "/test/active_nodes" to
        """
            []
        """
        Then zookeeper node "/test/active_nodes" should match json_exactly within "30" seconds
        """
            ["valkey1","valkey2","valkey3"]
        """ 
        When I run command on valkey host "valkey1"
        """
            CONFIG GET quorum-replicas
        """
        Then valkey cmd result should match regexp
        """
            .*valkey2.*
        """
        And valkey cmd result should match regexp
        """
            .*valkey3.*
        """
        And valkey cmd result should match regexp
        """
            .*192.168.234.14.*
        """
        And valkey cmd result should match regexp
        """
            .*192.168.234.15.*
        """
