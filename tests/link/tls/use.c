extern _Thread_local int shared;
extern _Thread_local long long wide;

void *addrFromOther(void) { return &shared; }
int readShared(void) { return shared; }
void writeShared(int v) { shared = v; }
long long readWide(void) { return wide; }
