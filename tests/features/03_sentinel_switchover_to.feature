Feature: Sentinel mode switchover to specified host

    Scenario: Sentinel mode switchover (to) with healthy master works
        Given sentinel shard is up and running
        Then zookeeper node "/test/health/redis1" should match json within "30" seconds
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
        When I run command on host "redis1"
        """
            rdsync switch --to redis2
        """
        Then command return code should be "0"
        And command output should match regexp
        """
            switchover done
        """
        And zookeeper node "/test/last_switch" should match json within "30" seconds
        """
        {
            "to": "redis2",
            "result": {
                "ok": true
            }
        }
        """
        And zookeeper node "/test/master" should match regexp within "30" seconds
        """
            redis2
        """
        And redis host "redis2" should be master
        And senticache host "redis1" should have master "redis2" within "30" seconds
        And senticache host "redis2" should have master "redis2" within "30" seconds
        And senticache host "redis3" should have master "redis2" within "30" seconds

    Scenario: Sentinel mode switchover (to) works with dead replica
        Given sentinel shard is up and running
        Then zookeeper node "/test/health/redis1" should match json within "30" seconds
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
        When redis on host "redis3" is stopped
        Then zookeeper node "/test/health/redis3" should match json within "30" seconds
        """
        {
            "ping_ok": false,
            "is_master": false
        }
        """
        And zookeeper node "/test/active_nodes" should match json_exactly within "60" seconds
        """
            ["redis1","redis2"]
        """
        When I run command on host "redis1"
        """
            rdsync switch --to redis2 --wait=0s
        """
        Then command return code should be "0"
        And command output should match regexp
        """
            switchover scheduled
        """
        And zookeeper node "/test/last_switch" should match json within "30" seconds
        """
        {
            "to": "redis2",
            "result": {
                "ok": true
            }
        }
        """
        And zookeeper node "/test/master" should match regexp within "30" seconds
        """
            redis2
        """
        And redis host "redis2" should be master
        And senticache host "redis1" should have master "redis2" within "30" seconds
        And senticache host "redis2" should have master "redis2" within "30" seconds
        And senticache host "redis3" should have master "redis2" within "30" seconds

    Scenario: Sentinel mode switchover to non-active host fails
        Given sentinel shard is up and running
        Then zookeeper node "/test/health/redis1" should match json within "30" seconds
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
        When redis on host "redis3" is stopped
        Then zookeeper node "/test/health/redis3" should match json within "30" seconds
        """
        {
            "ping_ok": false,
            "is_master": false
        }
        """
        And zookeeper node "/test/active_nodes" should match json_exactly within "60" seconds
        """
            ["redis1","redis2"]
        """
        When I run command on host "redis1"
        """
        rdsync switch --to redis3 --wait=0s
        """
        Then command return code should be "1"
        And command output should match regexp
        """
        redis3 is not active
        """
