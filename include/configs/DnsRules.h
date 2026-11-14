#pragma once

#include <QJsonArray>
#include <QJsonObject>
#include <QList>
#include <QString>
#include <QStringList>

// The DNS rules of the settings: the sites a rule lists resolve through its own server, through the proxy or directly.
// Sites take the NekoBox / v2ray forms, one per line: domain: (with subdomains, also a bare name), full:, keyword:,
// regexp: and geosite:<name> for a list of the rule-set catalog. Stored as the mobile app stores them.
namespace Configs {
    struct DnsRule {
        bool enabled = true;
        QStringList sites;
        QString server;
        bool direct = false;

        bool operator==(const DnsRule &other) const = default;
        // Why the core could not use the rule, empty when it can. A disabled rule is never checked.
        [[nodiscard]] QString Problem() const;
    };

    struct DnsRuleMatch {
        QJsonArray domains, suffixes, keywords, regexes;
        // Catalog names: geosite:x is the rule-set geosite-x.
        QStringList ruleSets;
        [[nodiscard]] bool IsEmpty() const {
            return domains.isEmpty() && suffixes.isEmpty() && keywords.isEmpty() && regexes.isEmpty() && ruleSets.isEmpty();
        }
    };

    // Anything unreadable reads as no rules, so a damaged setting never stops a connection.
    QList<DnsRule> DecodeDnsRules(const QString &stored);
    QString EncodeDnsRules(const QList<DnsRule> &rules);
    // One site per line or separated by commas; blank lines and # notes are left out.
    QStringList DnsRuleLines(const QString &text);
    DnsRuleMatch ParseDnsRuleSites(const QStringList &sites);

    // --self-test-dns-rules: 0 when the rules parse, store and validate as they should.
    int TestDnsRules();
}
