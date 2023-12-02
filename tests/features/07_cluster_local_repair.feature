Feature: Cluster mode local node repair

    Scenario: Cluster mode replica is restarted after OOM
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
        When redis on host "redis2" is killed
        And I wait for "300" seconds
        Then redis host "redis2" should become available within "120" seconds
        And redis host "redis2" should become replica of "redis1" within "60" seconds
        And replication on redis host "redis2" should run fine within "60" seconds

    Scenario: Cluster mode master is restarted after hanging
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
        When I run async command on host "redis1"
        """
            redis-cli -a functestpassword DEBUG SLEEP 600
        """
        And I wait for "300" seconds
        Then redis host "redis1" should become available within "60" seconds
