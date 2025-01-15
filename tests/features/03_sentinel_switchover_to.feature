Feature: Sentinel mode switchover to specified host

    Scenario: Sentinel mode switchover (to) with healthy master works
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
            rdsync switch --to valkey2
        """
        Then command return code should be "0"
        And command output should match regexp
        """
            switchover done
        """
        And zookeeper node "/test/last_switch" should match json within "30" seconds
        """
        {
            "to": "valkey2",
            "result": {
                "ok": true
            }
        }
        """
        And zookeeper node "/test/master" should match regexp within "30" seconds
        """
            valkey2
        """
        And valkey host "valkey2" should be master
        And senticache host "valkey1" should have master "valkey2" within "30" seconds
        And senticache host "valkey2" should have master "valkey2" within "30" seconds
        And senticache host "valkey3" should have master "valkey2" within "30" seconds

    Scenario: Sentinel mode switchover (to) works with dead replica
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
            rdsync switch --to valkey2 --wait=0s
        """
        Then command return code should be "0"
        And command output should match regexp
        """
            switchover scheduled
        """
        And zookeeper node "/test/last_switch" should match json within "30" seconds
        """
        {
            "to": "valkey2",
            "result": {
                "ok": true
            }
        }
        """
        And zookeeper node "/test/master" should match regexp within "30" seconds
        """
            valkey2
        """
        And valkey host "valkey2" should be master
        And senticache host "valkey1" should have master "valkey2" within "30" seconds
        And senticache host "valkey2" should have master "valkey2" within "30" seconds
        And senticache host "valkey3" should have master "valkey2" within "30" seconds

    Scenario: Sentinel mode switchover to non-active host fails
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
        rdsync switch --to valkey3 --wait=0s
        """
        Then command return code should be "1"
        And command output should match regexp
        """
        valkey3 is not active
        """
