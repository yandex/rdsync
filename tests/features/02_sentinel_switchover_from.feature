Feature: Sentinel mode switchover from old master

    Scenario: Sentinel mode switchover with healthy master works
        Given sentinel shard is up and running
        Then zookeeper node "/test/health/valkey1" should match json within "30" seconds
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
        When I run command on host "valkey1"
        """
            rdsync switch --from valkey1
        """
        Then command return code should be "0"
        And command output should match regexp
        """
            switchover done
        """
        And zookeeper node "/test/last_switch" should match json within "30" seconds
        """
        {
            "from": "valkey1",
            "result": {
                "ok": true
            }
        }
        """
        When I get zookeeper node "/test/master"
        And I save zookeeper query result as "new_master"
        Then valkey host "{{.new_master}}" should be master
        And senticache host "valkey1" should have master "{{.new_master}}" within "30" seconds
        And senticache host "valkey2" should have master "{{.new_master}}" within "30" seconds
        And senticache host "valkey3" should have master "{{.new_master}}" within "30" seconds
        When I wait for "30" seconds
        Then path "/var/lib/valkey/appendonlydir" exists on "valkey1"
        Then path "/var/lib/valkey/appendonlydir" does not exist on "{{.new_master}}"

    Scenario: Sentinel mode switchover with unhealthy replicas is rejected
        Given sentinel shard is up and running
        Then zookeeper node "/test/health/valkey1" should match json within "30" seconds
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
        When valkey on host "valkey3" is killed
        And valkey on host "valkey2" is killed
        And I run command on host "valkey1"
        """
            rdsync switch --from valkey1 --wait=0s
        """
        Then command return code should be "0"
        And command output should match regexp
        """
            switchover scheduled
        """
        And zookeeper node "/test/last_rejected_switch" should match json within "30" seconds
        """
        {
            "from": "valkey1",
            "to": "",
            "cause": "manual",
            "initiated_by": "valkey1",
            "result": {
                "ok": false,
                "error": "no quorum, have 0 replicas while 2 is required"
            }
        }
        """

    Scenario: Sentinel mode switchover with unhealthy replicas is not rejected if was approved before
        Given sentinel shard is up and running
        Then zookeeper node "/test/health/valkey1" should match json within "30" seconds
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
        When valkey on host "valkey3" is stopped
        And valkey on host "valkey2" is stopped
        And I set zookeeper node "/test/current_switch" to
        """
        {
            "from": "valkey1",
            "to": "",
            "cause": "manual",
            "initiated_by": "valkey1",
            "run_count": 1
        }
        """
        Then zookeeper node "/test/last_switch" should not exist within "30" seconds
        And zookeeper node "/test/last_rejected_switch" should not exist within "30" seconds
        When valkey on host "valkey3" is started
        And valkey on host "valkey2" is started
        Then zookeeper node "/test/last_switch" should match json within "60" seconds
        """
        {
            "from": "valkey1",
            "to": "",
            "cause": "manual",
            "initiated_by": "valkey1",
            "result": {
                "ok": true
            }
        }
        """
        When I get zookeeper node "/test/master"
        And I save zookeeper query result as "new_master"
        Then valkey host "{{.new_master}}" should be master
        And senticache host "valkey1" should have master "{{.new_master}}" within "30" seconds
        And senticache host "valkey2" should have master "{{.new_master}}" within "30" seconds
        And senticache host "valkey3" should have master "{{.new_master}}" within "30" seconds

    Scenario: Sentinel mode switchover works with dead replica
        Given sentinel shard is up and running
        Then zookeeper node "/test/health/valkey1" should match json within "30" seconds
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
        When valkey on host "valkey3" is stopped
        Then zookeeper node "/test/health/valkey3" should match json within "30" seconds
        """
        {
            "ping_ok": false,
            "is_master": false
        }
        """
        And zookeeper node "/test/active_nodes" should match json_exactly within "60" seconds
        """
            ["valkey1","valkey2"]
        """
        When I run command on host "valkey1"
        """
            rdsync switch --from valkey1 --wait=0s
        """
        Then command return code should be "0"
        And command output should match regexp
        """
            switchover scheduled
        """
        And zookeeper node "/test/last_switch" should match json within "30" seconds
        """
        {
            "from": "valkey1",
            "result": {
                "ok": true
            }
        }
        """
        When I get zookeeper node "/test/master"
        And I save zookeeper query result as "new_master"
        Then valkey host "{{.new_master}}" should be master
        And senticache host "valkey1" should have master "{{.new_master}}" within "30" seconds
        And senticache host "valkey2" should have master "{{.new_master}}" within "30" seconds
        And senticache host "valkey3" should have master "{{.new_master}}" within "30" seconds

    Scenario: Sentinel mode switchover (from) with read-only fs master works
        Given sentinel shard is up and running
        Then zookeeper node "/test/health/valkey1" should match json within "30" seconds
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
        When I run command on host "valkey1"
        """
            chattr +i /etc/valkey/valkey.conf
        """
        And I run command on host "valkey1"
        """
            rdsync switch --from valkey1
        """
        Then command return code should be "0"
        And command output should match regexp
        """
            switchover done
        """
        And zookeeper node "/test/last_switch" should match json within "30" seconds
        """
        {
            "from": "valkey1",
            "result": {
                "ok": true
            }
        }
        """
        When I get zookeeper node "/test/master"
        And I save zookeeper query result as "new_master"
        Then valkey host "{{.new_master}}" should be master
        And senticache host "valkey1" should have master "{{.new_master}}" within "30" seconds
        And senticache host "valkey2" should have master "{{.new_master}}" within "30" seconds
        And senticache host "valkey3" should have master "{{.new_master}}" within "30" seconds
        # Just to make docker cleanup happy
        When I run command on host "valkey1"
        """
            chattr -i /etc/valkey/valkey.conf
        """
