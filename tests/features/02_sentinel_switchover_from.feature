Feature: Sentinel mode switchover from old master

    Scenario: Sentinel mode switchover with healthy master works
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
            rdsync switch --from redis1
        """
        Then command return code should be "0"
        And command output should match regexp
        """
            switchover done
        """
        And zookeeper node "/test/last_switch" should match json within "30" seconds
        """
        {
            "from": "redis1",
            "result": {
                "ok": true
            }
        }
        """
        When I get zookeeper node "/test/master"
        And I save zookeeper query result as "new_master"
        Then redis host "{{.new_master}}" should be master
        And senticache host "redis1" should have master "{{.new_master}}" within "30" seconds
        And senticache host "redis2" should have master "{{.new_master}}" within "30" seconds
        And senticache host "redis3" should have master "{{.new_master}}" within "30" seconds
        When I wait for "30" seconds
        Then path "/var/lib/redis/appendonlydir" exists on "redis1"
        Then path "/var/lib/redis/appendonlydir" does not exist on "{{.new_master}}"

    Scenario: Sentinel mode switchover with unhealthy replicas is rejected
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
        When redis on host "redis3" is killed
        And redis on host "redis2" is killed
        And I run command on host "redis1"
        """
            rdsync switch --from redis1 --wait=0s
        """
        Then command return code should be "0"
        And command output should match regexp
        """
            switchover scheduled
        """
        And zookeeper node "/test/last_rejected_switch" should match json within "30" seconds
        """
        {
            "from": "redis1",
            "to": "",
            "cause": "manual",
            "initiated_by": "redis1",
            "result": {
                "ok": false,
                "error": "no quorum, have 0 replicas while 2 is required"
            }
        }
        """

    Scenario: Sentinel mode switchover with unhealthy replicas is not rejected if was approved before
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
        And redis on host "redis2" is stopped
        And I set zookeeper node "/test/current_switch" to
        """
        {
            "from": "redis1",
            "to": "",
            "cause": "manual",
            "initiated_by": "redis1",
            "run_count": 1
        }
        """
        Then zookeeper node "/test/last_switch" should not exist within "30" seconds
        And zookeeper node "/test/last_rejected_switch" should not exist within "30" seconds
        When redis on host "redis3" is started
        And redis on host "redis2" is started
        Then zookeeper node "/test/last_switch" should match json within "60" seconds
        """
        {
            "from": "redis1",
            "to": "",
            "cause": "manual",
            "initiated_by": "redis1",
            "result": {
                "ok": true
            }
        }
        """
        When I get zookeeper node "/test/master"
        And I save zookeeper query result as "new_master"
        Then redis host "{{.new_master}}" should be master
        And senticache host "redis1" should have master "{{.new_master}}" within "30" seconds
        And senticache host "redis2" should have master "{{.new_master}}" within "30" seconds
        And senticache host "redis3" should have master "{{.new_master}}" within "30" seconds

    Scenario: Sentinel mode switchover works with dead replica
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
            rdsync switch --from redis1 --wait=0s
        """
        Then command return code should be "0"
        And command output should match regexp
        """
            switchover scheduled
        """
        And zookeeper node "/test/last_switch" should match json within "30" seconds
        """
        {
            "from": "redis1",
            "result": {
                "ok": true
            }
        }
        """
        When I get zookeeper node "/test/master"
        And I save zookeeper query result as "new_master"
        Then redis host "{{.new_master}}" should be master
        And senticache host "redis1" should have master "{{.new_master}}" within "30" seconds
        And senticache host "redis2" should have master "{{.new_master}}" within "30" seconds
        And senticache host "redis3" should have master "{{.new_master}}" within "30" seconds

    Scenario: Sentinel mode switchover (from) with read-only fs master works
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
            chattr +i /etc/redis/redis.conf
        """
        And I run command on host "redis1"
        """
            rdsync switch --from redis1
        """
        Then command return code should be "0"
        And command output should match regexp
        """
            switchover done
        """
        And zookeeper node "/test/last_switch" should match json within "30" seconds
        """
        {
            "from": "redis1",
            "result": {
                "ok": true
            }
        }
        """
        When I get zookeeper node "/test/master"
        And I save zookeeper query result as "new_master"
        Then redis host "{{.new_master}}" should be master
        And senticache host "redis1" should have master "{{.new_master}}" within "30" seconds
        And senticache host "redis2" should have master "{{.new_master}}" within "30" seconds
        And senticache host "redis3" should have master "{{.new_master}}" within "30" seconds
        # Just to make docker cleanup happy
        When I run command on host "redis1"
        """
            chattr -i /etc/redis/redis.conf
        """
