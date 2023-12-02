Feature: Sentinel mode smoke tests

    Scenario: Sentinel mode initially works
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
        And senticache host "redis1" should have master "redis1" within "30" seconds
        And senticache host "redis2" should have master "redis1" within "30" seconds
        And senticache host "redis3" should have master "redis1" within "30" seconds
