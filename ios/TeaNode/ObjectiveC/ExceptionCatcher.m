#import "ExceptionCatcher.h"

@implementation ExceptionCatcher

+ (BOOL)catchException:(NS_NOESCAPE void (^)(void))work error:(NSError *_Nullable *_Nullable)error {
    @try {
        work();
        return YES;
    } @catch (NSException *exception) {
        if (error) {
            NSString *reason = exception.reason ?: exception.name;
            *error = [NSError errorWithDomain:exception.name code:0 userInfo:@{NSLocalizedDescriptionKey : reason}];
        }
        return NO;
    }
}

@end
