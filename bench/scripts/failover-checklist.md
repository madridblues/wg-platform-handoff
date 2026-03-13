# Failover Checklist

1. Start active tunnel through relay A.
2. Force relay A down (service stop/network drop).
3. Measure:
   - handshake recovery time
   - reconnect success
   - packet loss during transition
4. Verify audit events recorded for failover.
5. Repeat for VM and Fly pilot paths.

