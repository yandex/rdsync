package jepsen.rdsync.waitquorum;

import redis.clients.jedis.commands.ProtocolCommand;
import redis.clients.jedis.util.SafeEncoder;

public enum WaitQuorumCommand implements ProtocolCommand {
    WAITQUORUM("WAITQUORUM");

    private final byte[] raw;

    private WaitQuorumCommand(String name) {
        raw = SafeEncoder.encode(name);
    }

    @Override
    public byte[] getRaw() {
        return raw;
    }
}
