#import <Foundation/Foundation.h>

NS_ASSUME_NONNULL_BEGIN

/// ExceptionCatcher turns an Objective-C exception into an error. AVFAudio
/// raises them where Swift expects errors (a format it will not take, voice
/// processing changed mid-route), and Swift cannot catch one: uncaught, it
/// ends the app.
@interface ExceptionCatcher : NSObject

+ (BOOL)catchException:(NS_NOESCAPE void (^)(void))work error:(NSError *_Nullable *_Nullable)error;

@end

NS_ASSUME_NONNULL_END
