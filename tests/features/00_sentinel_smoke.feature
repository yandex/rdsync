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
        And path "/var/lib/redis/appendonlydir" does not exist on "redis1"
        And path "/var/lib/redis/appendonlydir" exists on "redis2"
        And path "/var/lib/redis/appendonlydir" exists on "redis3"

    Scenario: Sentinel mode duplicate ip resolve does not break rdsync
        Given sentinel shard is up and running
        When I run command on host "redis1"
        """
            echo '192.168.234.14 redis2 test1' >> /etc/hosts
            echo '192.168.234.14 redis2 test2' >> /etc/hosts
            echo '192.168.234.15 redis3 test3' >> /etc/hosts
            echo '192.168.234.15 redis3 test4' >> /etc/hosts
        """
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
        When I run command on host "redis3"
        """
            supervisorctl stop rdsync
        """
        And I run command on host "redis2"
        """
            supervisorctl stop rdsync
        """
        And I run command on host "redis1"
        """
            supervisorctl stop rdsync
        """
        And I run command on redis host "redis1"
        """
            CONFIG SET quorum-replicas redis2:6379
        """
        And I run command on host "redis1"
        """
            supervisorctl start rdsync
        """
        And I run command on host "redis2"
        """
            supervisorctl start rdsync
        """
        And I run command on host "redis3"
        """
            supervisorctl start rdsync
        """
        When I set zookeeper node "/test/active_nodes" to
        """
            []
        """
        Then zookeeper node "/test/active_nodes" should match json_exactly within "30" seconds
        """
            ["redis1","redis2","redis3"]
        """ 
        When I run command on redis host "redis1"
        """
            CONFIG GET quorum-replicas
        """
        Then redis cmd result should match regexp
        """
            .*redis2.*
        """
        And redis cmd result should match regexp
        """
            .*redis3.*
        """
        And redis cmd result should match regexp
        """
            .*192.168.234.14.*
        """
        And redis cmd result should match regexp
        """
            .*192.168.234.15.*
        """
