// Standalone native observation tool. No production admission override.
#include <sys/socket.h>
#include <sys/syscall.h>
#include <sys/un.h>
#include <netinet/in.h>
#include <arpa/inet.h>
#include <unistd.h>
#include <fcntl.h>
#include <pthread.h>
#include <signal.h>
#include <errno.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <stddef.h>
#include <stdint.h>
#include <time.h>
#include <poll.h>

struct msg_x {
    void *name; uint32_t namelen;
    struct iovec *iov; int32_t iovlen;
    void *control; uint32_t controllen; int32_t flags; uint64_t datalen;
};
_Static_assert(sizeof(struct msg_x) == 56, "msg_x ABI");
_Static_assert(offsetof(struct msg_x, iov) == 16, "iov ABI");
_Static_assert(offsetof(struct msg_x, control) == 32, "control ABI");
_Static_assert(offsetof(struct msg_x, datalen) == 48, "datalen ABI");
static volatile sig_atomic_t signals_seen;
static pthread_t caller;
static void handler(int sig) { (void)sig; signals_seen++; }
static void *interrupt_call(void *p) {
    (void)p;
    struct timespec delay = {.tv_nsec=100000000};
    nanosleep(&delay, NULL);
    int e = pthread_kill(caller, SIGUSR1);
    if (e) { fprintf(stderr, "pthread_kill: %s\n", strerror(e)); _exit(2); }
    return NULL;
}
static void check(int ok, const char *what) {
    if (!ok) { perror(what); exit(2); }
}
static double now(void) {
    struct timespec t; check(clock_gettime(CLOCK_MONOTONIC, &t)==0,"clock");
    return (double)t.tv_sec + (double)t.tv_nsec/1e9;
}
static int capacity(int fd, int option, int requested) {
    check(setsockopt(fd,SOL_SOCKET,option,&requested,sizeof(requested))==0,"set capacity");
    int actual=0; socklen_t n=sizeof(actual);
    check(getsockopt(fd,SOL_SOCKET,option,&actual,&n)==0,"get capacity");
    return actual;
}
static int receiver(int family, struct sockaddr_storage *addr, socklen_t *len, const char *path) {
    int fd=socket(family,SOCK_DGRAM,0); check(fd>=0,"receiver socket");
    memset(addr,0,sizeof(*addr));
    if(family==AF_INET) {
        struct sockaddr_in *a=(void*)addr; a->sin_len=sizeof(*a); a->sin_family=family;
        a->sin_addr.s_addr=htonl(INADDR_LOOPBACK); *len=sizeof(*a);
        int one=1; check(setsockopt(fd,IPPROTO_IP,IP_RECVTOS,&one,sizeof(one))==0,"IP_RECVTOS");
    } else if(family==AF_INET6) {
        struct sockaddr_in6 *a=(void*)addr; a->sin6_len=sizeof(*a); a->sin6_family=family;
        a->sin6_addr=in6addr_loopback; *len=sizeof(*a);
        int one=1; check(setsockopt(fd,IPPROTO_IPV6,IPV6_RECVTCLASS,&one,sizeof(one))==0,"IPV6_RECVTCLASS");
    } else {
        struct sockaddr_un *a=(void*)addr; a->sun_family=family;
        check(strlen(path)<sizeof(a->sun_path),"unix path length"); strcpy(a->sun_path,path);
        a->sun_len=(uint8_t)(offsetof(struct sockaddr_un,sun_path)+strlen(path)+1); *len=a->sun_len;
    }
    check(bind(fd,(void*)addr,*len)==0,"bind");
    check(getsockname(fd,(void*)addr,len)==0,"getsockname");
    check(fcntl(fd,F_SETFL,O_NONBLOCK)==0,"receiver nonblock");
    return fd;
}
int main(int argc,char **argv) {
    if(argc!=5) { fprintf(stderr,"usage: probe ipv4|ipv6|unix EAGAIN|EINTR|ENOBUFS|positive 0|1 batch|single\n"); return 2; }
    setbuf(stdout,NULL); alarm(5);
    int family=!strcmp(argv[1],"ipv4")?AF_INET:!strcmp(argv[1],"ipv6")?AF_INET6:AF_UNIX;
    int prefix=atoi(argv[3]); int single=!strcmp(argv[4],"single");
    int positive=!strcmp(argv[2],"positive"); int interrupt=!strcmp(argv[2],"EINTR");
    int expected_errno=interrupt?EINTR:family==AF_UNIX?ENOBUFS:EAGAIN;
    printf("case family=%s error=%s prefix=%d method=%s\n",argv[1],argv[2],prefix,argv[4]);
    char directory[]="/tmp/qerr.XXXXXX", paths[3][104];
    check(mkdtemp(directory)!=NULL,"mkdtemp");
    for(int i=0;i<3;i++) snprintf(paths[i],sizeof(paths[i]),"%s/%d",directory,i);
    struct sockaddr_storage good,bad,source; socklen_t goodlen=0,badlen=0,sourcelen=0;
    int rgood=receiver(family,&good,&goodlen,paths[0]);
    int rbad=family==AF_UNIX?receiver(family,&bad,&badlen,paths[1]):rgood;
    if(family!=AF_UNIX) { bad=good; badlen=goodlen; }
    int sender=family==AF_UNIX?receiver(family,&source,&sourcelen,paths[2]):socket(family,SOCK_DGRAM,0);
    check(sender>=0,"sender socket");
    int b=capacity(sender,SO_SNDBUF,family==AF_UNIX?16384:4096);
    int r=family==AF_UNIX?capacity(rbad,SO_RCVBUF,1024):0;
    size_t failing_size=family==AF_UNIX?(size_t)r+256:(size_t)b;
    check(b>0&&b<60000&&failing_size<60000,"bounded capacity");
    if(family==AF_UNIX) check(failing_size<(size_t)b,"Unix payload fits sender");
    check(fcntl(sender,F_SETFL,interrupt?0:O_NONBLOCK)==0,"sender flags");
    struct timeval timeout={.tv_sec=2};
    check(setsockopt(sender,SOL_SOCKET,SO_SNDTIMEO,&timeout,sizeof(timeout))==0,"send timeout");
    union { struct cmsghdr align; char bytes[CMSG_SPACE(sizeof(int))]; } control={0};
    struct cmsghdr *cm=(void*)control.bytes;
    cm->cmsg_len=CMSG_LEN(sizeof(int)); cm->cmsg_level=family==AF_INET?IPPROTO_IP:IPPROTO_IPV6;
    cm->cmsg_type=family==AF_INET?IP_TOS:IPV6_TCLASS;
    int ect0=2; memcpy(CMSG_DATA(cm),&ect0,sizeof(ect0));
    char small[]="accepted-prefix-ECT0";
    char *large=malloc(failing_size); check(large!=NULL,"malloc"); memset(large,'Z',failing_size);
    struct iovec iov[2]={{.iov_base=small,.iov_len=sizeof(small)},{.iov_base=large,.iov_len=failing_size}};
    struct msg_x messages[2]={0};
    for(int i=0;i<2;i++) {
        messages[i].name=i==0?(void*)&good:(void*)&bad;
        messages[i].namelen=i==0?goodlen:badlen; messages[i].iov=&iov[i]; messages[i].iovlen=1;
        if(family!=AF_UNIX) { messages[i].control=control.bytes; messages[i].controllen=sizeof(control.bytes); }
    }
    struct msg_x *start=positive||prefix?messages:&messages[1]; int count=prefix?2:1;
    printf("sndbuf=%d rcvbuf=%d suffix_bytes=%zu control_bytes=%u\n",b,r,failing_size,messages[1].controllen);
    pthread_t worker; caller=pthread_self();
    if(interrupt) {
        struct sigaction sa={0}; sa.sa_handler=handler; sigemptyset(&sa.sa_mask);
        check(sigaction(SIGUSR1,&sa,NULL)==0,"sigaction");
        check(pthread_create(&worker,NULL,interrupt_call,NULL)==0,"pthread_create");
    }
    errno=0; double before=now(); long result;
    if(single) {
        struct msghdr ordinary={.msg_name=start->name,.msg_namelen=start->namelen,
            .msg_iov=start->iov,.msg_iovlen=1,.msg_control=start->control,.msg_controllen=start->controllen};
        result=sendmsg(sender,&ordinary,0);
    } else result=syscall(SYS_sendmsg_x,sender,start,count,0);
    int err=errno; double elapsed=now()-before; int signals_at_return=signals_seen;
    if(interrupt) check(pthread_join(worker,NULL)==0,"pthread_join");
    printf("result=%ld errno=%d errno_text=%s elapsed_ms=%.3f signals_at_return=%d signals_final=%d\n",result,err,strerror(err),elapsed*1000,signals_at_return,signals_seen);
    int received=0,bad_packets=0; double deadline=now()+0.250;
    while(now()<deadline) {
        struct pollfd fds[2]={{.fd=rgood,.events=POLLIN},{.fd=rbad==rgood?-1:rbad,.events=POLLIN}};
        int ready=poll(fds,2,10); if(ready<0&&errno==EINTR) continue; check(ready>=0,"poll");
        for(int i=0;i<2;i++) if(fds[i].revents&POLLIN) {
            char payload[65536]; union { struct cmsghdr align; char bytes[256]; } oob;
            struct iovec data={.iov_base=payload,.iov_len=sizeof(payload)};
            struct msghdr msg={.msg_iov=&data,.msg_iovlen=1,.msg_control=oob.bytes,.msg_controllen=sizeof(oob.bytes)};
            ssize_t n=recvmsg(fds[i].fd,&msg,0); check(n>=0,"recvmsg");
            int tos=-1;
            for(struct cmsghdr *c=CMSG_FIRSTHDR(&msg);c;c=CMSG_NXTHDR(&msg,c)) {
                if(c->cmsg_level==IPPROTO_IP&&c->cmsg_type==IP_RECVTOS) tos=*(unsigned char*)CMSG_DATA(c);
                if(c->cmsg_level==IPPROTO_IPV6&&c->cmsg_type==IPV6_TCLASS) memcpy(&tos,CMSG_DATA(c),sizeof(tos));
            }
            int exact=n==(ssize_t)sizeof(small)&&memcmp(payload,small,sizeof(small))==0;
            printf("peer=%s bytes=%zd exact_prefix=%d tos=%d flags=%d\n",i==0?"good":"bad",n,exact,tos,msg.msg_flags);
            received++; if(!exact||i!=0||msg.msg_flags||(family!=AF_UNIX&&(tos&3)!=2)) bad_packets++;
        }
    }
    int expected_packets=(positive||prefix)?1:0;
    int valid=received==expected_packets&&bad_packets==0;
    if(positive) valid &= result==(single?(long)sizeof(small):1)&&err==0;
    else if(prefix) valid &= !single&&result==1&&err==0;
    else valid &= result==-1&&err==expected_errno;
    if(interrupt) valid &= signals_at_return==1&&elapsed>=0.050&&elapsed<1.5;
    printf("received=%d bad_packets=%d verdict=%s\n",received,bad_packets,valid?"MATCHED_PREDICTION":"INCONCLUSIVE");
    close(sender); close(rgood); if(rbad!=rgood) close(rbad); free(large);
    for(int i=0;i<3;i++) unlink(paths[i]); rmdir(directory);
    return valid?0:1;
}
