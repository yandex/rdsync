Feature: Sentinel mode maintenance tests

    Scenario: Sentinel mode maintenance control via dcs
        Given sentinel shard is up and running
        Then zookeeper node "/test/active_nodes" should match json_exactly within "30" seconds
        """
            ["redis1","redis2","redis3"]
        """
        And senticache host "redis1" should have master "redis1" within "30" seconds
        And senticache host "redis2" should have master "redis1" within "30" seconds
        And senticache host "redis3" should have master "redis1" within "30" seconds
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
            ["redis1","redis2","redis3"]
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
        And senticache host "redis1" should have master "redis1" within "30" seconds
        And senticache host "redis2" should have master "redis1" within "30" seconds
        And senticache host "redis3" should have master "redis1" within "30" seconds
        When I delete zookeeper node "/test/maintenance"
        Then zookeeper node "/test/active_nodes" should match json_exactly within "30" seconds
        """
            ["redis1","redis2","redis3"]
        """

    Scenario: Sentinel mode maintenance enter sets min-replicas-to-write to 0 on master
        Given sentinel shard is up and running
        Then zookeeper node "/test/active_nodes" should match json_exactly within "30" seconds
        """
            ["redis1","redis2","redis3"]
        """
        When I wait for "60" seconds
        And I run command on redis host "redis1"
        """
            CONFIG GET min-replicas-to-write
        """
        Then redis cmd result should match regexp
        """
            .*min-replicas-to-write 1.*
        """
        When I run command on host "redis1"
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
        When I run command on redis host "redis1"
        """
            CONFIG GET min-replicas-to-write
        """
        Then redis cmd result should match regexp
        """
            .*min-replicas-to-write *0.*
        """

    Scenario: Sentinel mode maintenance leave updates master host in DCS after manual change
        Given sentinel shard is up and running
        Then zookeeper node "/test/active_nodes" should match json_exactly within "30" seconds
        """
            ["redis1","redis2","redis3"]
        """
        When I run command on host "redis1"
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
        And redis host "redis1" should become replica of "redis2" within "15" seconds
        And replication on redis host "redis1" should run fine within "15" seconds
        And redis host "redis3" should become replica of "redis2" within "15" seconds
        And replication on redis host "redis3" should run fine within "15" seconds
        When I run command on host "redis1"
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
            "redis2"
        """
        And senticache host "redis1" should have master "redis2" within "30" seconds
        And senticache host "redis2" should have master "redis2" within "30" seconds
        And senticache host "redis3" should have master "redis2" within "30" seconds
        When I run command on host "redis1"
        """
            rdsync maintenance
        """
        Then command return code should be "0"
        And command output should match regexp
        """
            off
        """

    Scenario: Sentinel mode maintenance does not stop on DCS failure
        Given sentinel shard is up and running
        Then zookeeper node "/test/active_nodes" should match json_exactly within "30" seconds
        """
            ["redis1","redis2","redis3"]
        """
        And senticache host "redis1" should have master "redis1" within "30" seconds
        And senticache host "redis2" should have master "redis1" within "30" seconds
        And senticache host "redis3" should have master "redis1" within "30" seconds
        When I run command on host "redis1"
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
        When I run command on host "redis1"
        """
            rdsync info
        """
        Then command return code should be "1"
        And senticache host "redis1" should have master "redis1" within "30" seconds
        And senticache host "redis2" should have master "redis1" within "30" seconds
        And senticache host "redis3" should have master "redis1" within "30" seconds
        When I wait for "30" seconds
        Then redis host "redis1" should be master
        And redis host "redis2" should be replica of "redis1"
        And redis host "redis3" should be replica of "redis1"
        When I run command on host "redis1" with timeout "20" seconds
        """
            supervisorctl restart rdsync
        """
        Then command return code should be "0"
        When I run command on host "redis2" with timeout "20" seconds
        """
            supervisorctl restart rdsync
        """
        Then command return code should be "0"
        When I run command on host "redis2" with timeout "20" seconds
        """
            supervisorctl restart rdsync
        """
        Then command return code should be "0"
        When I wait for "30" seconds
        Then redis host "redis1" should be master
        And redis host "redis2" should be replica of "redis1"
        And redis host "redis3" should be replica of "redis1"
        When host "zoo3" is attached to the network
        And host "zoo2" is attached to the network
        And host "zoo1" is attached to the network
        Then zookeeper node "/test/maintenance" should match json within "90" seconds
        """
        {
            "initiated_by": "redis1"
        }
        """
        When I run command on host "redis1"
        """
            rdsync maint off
        """
        Then command return code should be "0"
        And redis host "redis1" should be master
        And redis host "redis2" should be replica of "redis1"
        And redis host "redis3" should be replica of "redis1"
        And zookeeper node "/test/health/redis1" should match json within "30" seconds
        """
        {
            "ping_ok": true,
            "is_master": true,
            "is_read_only": false
        }
        """
        And zookeeper node "/test/health/redis2" should match json within "30" seconds
        """
        {
            "ping_ok": true,
            "is_master": false
        }
        """
        And zookeeper node "/test/health/redis3" should match json within "30" seconds
        """
        {
            "ping_ok": true,
            "is_master": false
        }
        """
        And senticache host "redis1" should have master "redis1" within "30" seconds
        And senticache host "redis2" should have master "redis1" within "30" seconds
        And senticache host "redis3" should have master "redis1" within "30" seconds
