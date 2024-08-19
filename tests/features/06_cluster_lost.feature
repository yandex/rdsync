Feature: Cluster mode survives dcs conn loss

    Scenario: Cluster mode survives dcs conn loss
        Given clustered shard is up and running
        Then redis host "redis1" should be master
        And redis host "redis2" should become replica of "redis1" within "15" seconds
        And replication on redis host "redis2" should run fine within "15" seconds
        And redis host "redis3" should become replica of "redis1" within "15" seconds
        And replication on redis host "redis3" should run fine within "15" seconds
        And zookeeper node "/test/active_nodes" should match json_exactly within "30" seconds
        """
            ["redis1","redis2","redis3"]
        """
        When host "zoo3" is detached from the network
        And host "zoo2" is detached from the network
        And host "zoo1" is detached from the network
        Then redis host "redis1" should be master
        And redis host "redis2" should become replica of "redis1" within "15" seconds
        And replication on redis host "redis2" should run fine within "15" seconds
        And redis host "redis3" should become replica of "redis1" within "15" seconds
        And replication on redis host "redis3" should run fine within "15" seconds
        When I run command on redis host "redis1"
        """
            SET MYKEY TESTVALUE
        """
        Then redis cmd result should match regexp
        """
            OK
        """

    Scenario: Cluster mode partitioned master goes offline
        Given clustered shard is up and running
        Then redis host "redis1" should be master
        And redis host "redis2" should become replica of "redis1" within "15" seconds
        And replication on redis host "redis2" should run fine within "15" seconds
        And redis host "redis3" should become replica of "redis1" within "15" seconds
        And replication on redis host "redis3" should run fine within "15" seconds
        And zookeeper node "/test/active_nodes" should match json_exactly within "30" seconds
        """
            ["redis1","redis2","redis3"]
        """
        When host "zoo3" is detached from the network
        And host "zoo2" is detached from the network
        And host "zoo1" is detached from the network
        And host "redis2" is detached from the network
        And host "redis3" is detached from the network
        Then redis host "redis1" should become unavailable within "30" seconds
        When host "zoo3" is attached to the network
        And host "zoo2" is attached to the network
        And host "zoo1" is attached to the network
        And host "redis2" is attached to the network
        And host "redis3" is attached to the network
        Then redis host "redis1" should become available within "60" seconds

    Scenario: Cluster mode partitioned replica goes offline
        Given clustered shard is up and running
        Then redis host "redis1" should be master
        And redis host "redis2" should become replica of "redis1" within "15" seconds
        And replication on redis host "redis2" should run fine within "15" seconds
        And redis host "redis3" should become replica of "redis1" within "15" seconds
        And replication on redis host "redis3" should run fine within "15" seconds
        And zookeeper node "/test/active_nodes" should match json_exactly within "30" seconds
        """
            ["redis1","redis2","redis3"]
        """
        When host "zoo3" is detached from the network
        And host "zoo2" is detached from the network
        And host "zoo1" is detached from the network
        And host "redis1" is detached from the network
        And host "redis3" is detached from the network
        Then redis host "redis2" should become unavailable within "30" seconds
        When host "zoo3" is attached to the network
        And host "zoo2" is attached to the network
        And host "zoo1" is attached to the network
        And host "redis1" is attached to the network
        And host "redis3" is attached to the network
        Then redis host "redis2" should become available within "60" seconds

    Scenario: Cluster mode partially partitioned manager gives up on manager role
        Given clustered shard is up and running
        Then redis host "redis1" should be master
        And redis host "redis2" should become replica of "redis1" within "15" seconds
        And replication on redis host "redis2" should run fine within "15" seconds
        And redis host "redis3" should become replica of "redis1" within "15" seconds
        And replication on redis host "redis3" should run fine within "15" seconds
        And zookeeper node "/test/active_nodes" should match json_exactly within "30" seconds
        """
            ["redis1","redis2","redis3"]
        """
        When I run command on host "redis1" with timeout "20" seconds
        """
            supervisorctl stop rdsync
        """
        Then command return code should be "0"
        And  zookeeper node "/test/manager" should match regexp within "30" seconds
        """
            .*redis[23].*
        """
        When I run command on host "redis1" with timeout "20" seconds
        """
            supervisorctl start rdsync
        """
        When I get zookeeper node "/test/manager"
        And I save zookeeper query result as "new_manager"
        And port "6379" on host "{{.new_manager.hostname}}" is blocked
        And I wait for "60" seconds
        Then redis host "redis1" should be master
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
            ["redis1","redis2","redis3"]
        """
