import AuthenticationServices
import UIKit

final class MobileAuthentication: NSObject, ASAuthorizationControllerDelegate,
    ASAuthorizationControllerPresentationContextProviding, ASWebAuthenticationPresentationContextProviding {
    static var appleEnabled: Bool {
        Bundle.main.object(forInfoDictionaryKey: "GofastrSignInWithApple") as? Bool == true
    }

    private let anchor: ASPresentationAnchor
    private var apple: ASAuthorizationController?
    private var web: ASWebAuthenticationSession?
    private var completion: ((MobileServiceResult) -> Void)?

    init(anchor: ASPresentationAnchor, completion: @escaping (MobileServiceResult) -> Void) {
        self.anchor = anchor
        self.completion = completion
    }

    func start(_ request: MobileAuthenticationRequest) {
        if request.operation == "apple" {
            guard Self.appleEnabled else {
                finish(MobileServiceResult(status: "unsupported", message: "Sign in with Apple is not enabled."))
                return
            }
            let signIn = ASAuthorizationAppleIDProvider().createRequest()
            signIn.nonce = request.nonce
            let controller = ASAuthorizationController(authorizationRequests: [signIn])
            controller.delegate = self
            controller.presentationContextProvider = self
            apple = controller
            controller.performRequests()
            return
        }
        guard let url = request.authorize, let scheme = request.redirect?.scheme else {
            finish(MobileServiceResult(status: "failed", message: "The OAuth request is invalid."))
            return
        }
        let handler: ASWebAuthenticationSession.CompletionHandler = { [weak self] callback, error in
            DispatchQueue.main.async {
                guard let self else { return }
                if (error as? ASWebAuthenticationSessionError)?.code == .canceledLogin {
                    self.finish(Self.cancelled)
                } else if error == nil, let callback, let result = request.acceptCallback(callback) {
                    self.finish(MobileServiceResult(status: "completed", authentication: result))
                } else {
                    self.finish(MobileServiceResult(status: "failed", message: "OAuth did not return a valid authorization code."))
                }
            }
        }
        let session: ASWebAuthenticationSession
        if #available(iOS 17.4, *) {
            session = ASWebAuthenticationSession(url: url, callback: .customScheme(scheme), completionHandler: handler)
        } else {
            session = ASWebAuthenticationSession(url: url, callbackURLScheme: scheme, completionHandler: handler)
        }
        session.presentationContextProvider = self
        session.prefersEphemeralWebBrowserSession = request.preferEphemeralSession ?? false
        web = session
        if !session.start() {
            finish(MobileServiceResult(status: "failed", message: "The OAuth sign-in session could not start."))
        }
    }

    func presentationAnchor(for controller: ASAuthorizationController) -> ASPresentationAnchor { anchor }
    func presentationAnchor(for session: ASWebAuthenticationSession) -> ASPresentationAnchor { anchor }

    func authorizationController(controller: ASAuthorizationController, didCompleteWithAuthorization authorization: ASAuthorization) {
        DispatchQueue.main.async { self.accept(authorization) }
    }

    private func accept(_ authorization: ASAuthorization) {
        guard let credential = authorization.credential as? ASAuthorizationAppleIDCredential,
              let tokenData = credential.identityToken, let token = String(data: tokenData, encoding: .utf8),
              let codeData = credential.authorizationCode, let code = String(data: codeData, encoding: .utf8),
              MobileAuthenticationRequest.token(token, limit: 16384),
              MobileAuthenticationRequest.token(code, limit: 8192),
              MobileAuthenticationRequest.token(credential.user, limit: 512) else {
            finish(MobileServiceResult(status: "failed", message: "Apple did not return the required sign-in credentials."))
            return
        }
        finish(MobileServiceResult(status: "completed", authentication: MobileAuthenticationResult(
            authorizationCode: code, identityToken: token, user: credential.user)))
    }

    func authorizationController(controller: ASAuthorizationController, didCompleteWithError error: Error) {
        DispatchQueue.main.async {
            self.finish((error as? ASAuthorizationError)?.code == .canceled ? Self.cancelled :
                MobileServiceResult(status: "failed", message: "Sign in with Apple could not be completed."))
        }
    }

    func cancel() {
        apple?.cancel()
        web?.cancel()
        finish(Self.cancelled)
    }

    private static var cancelled: MobileServiceResult {
        MobileServiceResult(status: "cancelled", message: "Sign-in cancelled.")
    }

    private func finish(_ result: MobileServiceResult) {
        guard let completion else { return }
        self.completion = nil
        apple = nil
        web = nil
        completion(result)
    }
}
