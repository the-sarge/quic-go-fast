// Temporary issue #1210 software-interface observer. Not production code.
#include <pcap/pcap.h>
#include <errno.h>
#include <grp.h>
#include <limits.h>
#include <pwd.h>
#include <signal.h>
#include <stdbool.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/stat.h>
#include <time.h>
#include <unistd.h>

static volatile sig_atomic_t stopped;
static unsigned long long packets;
static void stop(int sig) { stopped = sig; }
static double mono(void) { struct timespec t; clock_gettime(CLOCK_MONOTONIC, &t); return t.tv_sec + t.tv_nsec / 1e9; }
static void packet(u_char *user, const struct pcap_pkthdr *h, const u_char *data) {
    pcap_dump(user, h, data); packets++;
}
static void fail(pcap_t *p, const char *message) {
    fprintf(stderr, "%s: %s\n", message, p ? pcap_geterr(p) : strerror(errno));
    if (p) pcap_close(p); exit(1);
}
int main(int argc, char **argv) {
    char path[PATH_MAX], error[PCAP_ERRBUF_SIZE]; struct stat st;
    const char *prefix = "/Volumes/worktrees/quic-go-fast/resolve-1210/.cache/1210/";
    if (argc != 2 || !realpath(argv[1], path) || strncmp(path,prefix,strlen(prefix)) || geteuid()!=0) return 2;
    struct passwd *owner=getpwnam("josh");
    if (!owner || stat(path,&st) || !S_ISDIR(st.st_mode) || st.st_uid!=owner->pw_uid) return 2;
    uid_t uid=owner->pw_uid; gid_t gid=owner->pw_gid;
    pcap_t *p=pcap_create("en8",error); if(!p) {fprintf(stderr,"%s\n",error);return 1;}
    if(pcap_set_snaplen(p,128) || pcap_set_promisc(p,0) || pcap_set_timeout(p,100) || pcap_set_buffer_size(p,4*1024*1024) || pcap_set_tstamp_type(p,PCAP_TSTAMP_HOST) || pcap_set_tstamp_precision(p,PCAP_TSTAMP_PRECISION_MICRO)) fail(p,"configure");
    int activated=pcap_activate(p); if(activated<0) fail(p,"activate");
    if(activated>0) fprintf(stderr,"activation warning %d: %s\n",activated,pcap_geterr(p));
    if(pcap_datalink(p)!=DLT_EN10MB || pcap_setnonblock(p,1,error)) fail(p,"Ethernet/nonblock");
    struct bpf_program filter;
    if(pcap_compile(p,&filter,"ip and udp and host 192.168.1.50 and host 192.168.1.110",1,PCAP_NETMASK_UNKNOWN)) fail(p,"compile filter");
    if(pcap_setfilter(p,&filter)) fail(p,"install filter"); pcap_freecode(&filter);
    // No privileged work follows opening/configuring the capture handle.
    if(setgroups(0,NULL) || setgid(gid) || setuid(uid) || getuid()!=uid || geteuid()!=uid) fail(p,"drop privileges");
    if(chdir(path)) fail(p,"capture directory");
    FILE *dumpfile=fopen("mac-interface.pcap","wx"); if(!dumpfile) fail(p,"create capture");
    pcap_dumper_t *dump=pcap_dump_fopen(p,dumpfile); if(!dump) fail(p,"open dump");
    FILE *log=fopen("tcpdump.log","wx"); if(!log) fail(p,"create log");
    FILE *info=fopen("collector.json","wx"); if(!info) fail(p,"create info");
    fprintf(info,"{\"pid\":%d,\"uid\":%d,\"timeout_seconds\":180,\"collector\":\"libpcap-nonblocking\",\"libpcap\":\"%s\"}\n",getpid(),getuid(),pcap_lib_version()); fclose(info);
    fprintf(log,"listening on en8; Ethernet; snaplen 128; host microseconds; nonblocking; uid %d\n",getuid());fflush(log);
    signal(SIGINT,stop); signal(SIGTERM,stop);
    double start=mono(); int result=0; const char *reason="requested";
    while(!stopped && access("stop",F_OK)!=0 && mono()-start<180) {
        int count=pcap_dispatch(p,4096,packet,(u_char *)dump);
        if(count<0) {result=1;reason="dispatch-error";fprintf(log,"%s\n",pcap_geterr(p));break;}
        if(count==0) {struct timespec pause={0,1000000};nanosleep(&pause,NULL);}
    }
    if(stopped) {result=1;reason="signal";}
    else if(mono()-start>=180) {result=1;reason="timeout";}
    // Flush buffered packets after stop; no new traffic is launched during this drain.
    for(int i=0;i<64;i++) {int count=pcap_dispatch(p,4096,packet,(u_char *)dump);if(count<=0)break;}
    struct pcap_stat stats; memset(&stats,0,sizeof(stats));
    if(pcap_stats(p,&stats)) {result=1;reason="stats-error";}
    if(pcap_dump_flush(dump)) {result=1;reason="flush-error";}
    fprintf(log,"%llu packets captured\n%u packets received by filter\n%u packets dropped by kernel\n%u packets dropped by interface\n",packets,stats.ps_recv,stats.ps_drop,stats.ps_ifdrop);fclose(log);
    pcap_dump_close(dump);pcap_close(p);
    FILE *receipt=fopen("collector-result.json","wx");if(!receipt)return 1;
    fprintf(receipt,"{\"exit\":%d,\"reason\":\"%s\",\"elapsed_seconds\":%.9f,\"uid\":%d,\"captured\":%llu,\"received\":%u,\"kernel_dropped\":%u,\"interface_dropped\":%u}\n",result,reason,mono()-start,getuid(),packets,stats.ps_recv,stats.ps_drop,stats.ps_ifdrop);fclose(receipt);return result;
}
