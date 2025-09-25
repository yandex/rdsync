Feature: Cluster mode maintenance tests

    Scenario: Cluster mode maintenance control via dcs
        Given clustered shard is up and running
        Then zookeeper node "/test/active_nodes" should match json_exactly within "30" seconds
        """
            ["valkey1","valkey2","valkey3"]
        """
        When I set zookeeper node "/test/maintenance" to
        """
        {
            "initiated_by": "test"
        }
        """
        Then zookeeper node "/test/maintenance" should match json within "30" seconds
        """
        {
            "rdsync_paused": true
        }
        """
        And zookeeper node "/test/active_nodes" should not exist
        When I set zookeeper node "/test/maintenance" to
        """
        {
            "initiated_by": "test",
            "should_leave": true
        }
        """
        Then zookeeper node "/test/maintenance" should not exist within "30" seconds
        And zookeeper node "/test/active_nodes" should match json_exactly
        """
            ["valkey1","valkey2","valkey3"]
        """
        When I set zookeeper node "/test/maintenance" to
        """
        {
            "initiated_by": "test"
        }
        """
        Then zookeeper node "/test/maintenance" should match json within "30" seconds
        """
        {
            "rdsync_paused": true
        }
        """
        And zookeeper node "/test/active_nodes" should not exist
        When I delete zookeeper node "/test/maintenance"
        Then zookeeper node "/test/active_nodes" should match json_exactly within "30" seconds
        """
            ["valkey1","valkey2","valkey3"]
        """

    Scenario: Cluster mode maintenance enter sets quorum-replicas-to-write to 0 on master
        Given clustered shard is up and running
        Then zookeeper node "/test/active_nodes" should match json_exactly within "30" seconds
        """
            ["valkey1","valkey2","valkey3"]
        """
        When I wait for "60" seconds
        And I run command on valkey host "valkey1"
        """
            CONFIG GET quorum-replicas-to-write
        """
        Then valkey cmd result should match regexp
        """
            .*quorum-replicas-to-write 1.*
        """
        When I run command on host "valkey1"
        """
            rdsync maint on
        """
        Then command return code should be "0"
        And command output should match regexp
        """
            maintenance enabled
        """
        And zookeeper node "/test/maintenance" should match json
        """
        {
            "rdsync_paused": true
        }
        """
        And zookeeper node "/test/active_nodes" should not exist
        When I run command on valkey host "valkey1"
        """
            CONFIG GET quorum-replicas-to-write
        """
        Then valkey cmd result should match regexp
        """
            .*quorum-replicas-to-write *0.*
        """

    Scenario: Cluster mode maintenance leave updates master host in DCS after manual change
        Given clustered shard is up and running
        Then zookeeper node "/test/active_nodes" should match json_exactly within "30" seconds
        """
            ["valkey1","valkey2","valkey3"]
        """
        When I run command on host "valkey1"
        """
            rdsync maint on
        """
        Then command return code should be "0"
        And command output should match regexp
        """
            maintenance enabled
        """
        And zookeeper node "/test/maintenance" should match json
        """
        {
            "rdsync_paused": true
        }
        """
        And zookeeper node "/test/active_nodes" should not exist
        When I run command on valkey host "valkey2"
        """
            CLUSTER FAILOVER
        """
        Then valkey cmd result should match regexp
        """
            .*OK.*
        """
        And valkey host "valkey1" should become replica of "valkey2" within "15" seconds
        And replication on valkey host "valkey1" should run fine within "15" seconds
        And valkey host "valkey3" should become replica of "valkey2" within "15" seconds
        And replication on valkey host "valkey3" should run fine within "15" seconds
        When I run command on host "valkey1"
        """
            rdsync mnt off
        """
        Then command return code should be "0"
        And command output should match regexp
        """
            maintenance disabled
        """
        Then zookeeper node "/test/master" should match json within "30" seconds
        """
            "valkey2"
        """
        When I run command on host "valkey1"
        """
            rdsync maintenance
        """
        Then command return code should be "0"
        And command output should match regexp
        """
            off
        """

    Scenario: Cluster mode maintenance does not stop on DCS failure
        Given clustered shard is up and running
        Then zookeeper node "/test/active_nodes" should match json_exactly within "30" seconds
        """
            ["valkey1","valkey2","valkey3"]
        """
        When I run command on host "valkey1"
        """
            rdsync maint on
        """
        Then command return code should be "0"
        And command output should match regexp
        """
            maintenance enabled
        """
        And zookeeper node "/test/maintenance" should match json
        """
        {
            "rdsync_paused": true
        }
        """
        And zookeeper node "/test/active_nodes" should not exist
        When host "zoo3" is detached from the network
        And host "zoo2" is detached from the network
        And host "zoo1" is detached from the network
        When I run command on host "valkey1" with timeout "300" seconds
        """
            rdsync info
        """
        Then command return code should be "1"
        When I wait for "30" seconds
        Then valkey host "valkey1" should be master
        And valkey host "valkey2" should be replica of "valkey1"
        And valkey host "valkey3" should be replica of "valkey1"
        When I run command on host "valkey1" with timeout "20" seconds
        """
            supervisorctl restart rdsync
        """
        Then command return code should be "0"
        When I run command on host "valkey2" with timeout "20" seconds
        """
            supervisorctl restart rdsync
        """
        Then command return code should be "0"
        When I run command on host "valkey2" with timeout "20" seconds
        """
            supervisorctl restart rdsync
        """
        Then command return code should be "0"
        When I wait for "30" seconds
        Then valkey host "valkey1" should be master
        And valkey host "valkey2" should be replica of "valkey1"
        And valkey host "valkey3" should be replica of "valkey1"
        When host "zoo3" is attached to the network
        And host "zoo2" is attached to the network
        And host "zoo1" is attached to the network
        Then zookeeper node "/test/maintenance" should match json within "90" seconds
        """
        {
            "initiated_by": "valkey1"
        }
        """
        When I run command on host "valkey1"
        """
            rdsync maint off
        """
        Then command return code should be "0"
        And valkey host "valkey1" should be master
        And valkey host "valkey2" should be replica of "valkey1"
        And valkey host "valkey3" should be replica of "valkey1"
        And zookeeper node "/test/health/valkey1" should match json within "30" seconds
        """
        {
            "ping_ok": true,
            "is_master": true,
            "is_read_only": false
        }
        """
        And zookeeper node "/test/health/valkey2" should match json within "30" seconds
        """
        {
            "ping_ok": true,
            "is_master": false
        }
        """
        And zookeeper node "/test/health/valkey3" should match json within "30" seconds
        """
        {
            "ping_ok": true,
            "is_master": false
        }
        """
